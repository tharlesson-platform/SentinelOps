package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sentinelops/sentinelops/internal/database"
)

// Dispatcher is a durable outbox consumer. It processes a deliberately small,
// idempotent webhook.received domain event and leaves delivery state available
// to SSE replay. External notification channels are added as distinct handlers.
type Dispatcher struct {
	Store        *database.Store
	Logger       *slog.Logger
	Interval     time.Duration
	HTTPClient   *http.Client
	WebhookURLs  map[string]string
	AllowedHosts []string
}

type routeSpec struct {
	Channel    string
	TargetRef  string
	Escalation *struct {
		After     string
		TargetRef string
	}
}

func (d *Dispatcher) Run(ctx context.Context) {
	interval := d.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		d.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) runOnce(ctx context.Context) {
	rows, err := d.Store.Pool.Query(ctx, "SELECT id::text FROM organizations ORDER BY id")
	if err != nil {
		d.Logger.Error("outbox organization query failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var organizationID string
		if err := rows.Scan(&organizationID); err == nil {
			d.dispatchTenant(database.WithTenant(ctx, organizationID), organizationID)
		}
	}
}

func (d *Dispatcher) dispatchTenant(ctx context.Context, organizationID string) {
	// Reclaim only a visibly abandoned lease. The handler itself is idempotent.
	_, _ = d.Store.Pool.Exec(ctx, `UPDATE outbox_events SET status='PENDING', locked_at=NULL,
last_error='worker interrupted while processing', available_at=now()
WHERE organization_id=$1 AND status='PROCESSING' AND locked_at < now() - interval '5 minutes'`, organizationID)
	for i := 0; i < 20; i++ {
		var id int64
		var eventType string
		var payload []byte
		tx, err := d.Store.Pool.Begin(ctx)
		if err != nil {
			d.Logger.Error("outbox begin failed", "error", err)
			return
		}
		err = tx.QueryRow(ctx, `SELECT id,event_type,payload FROM outbox_events
WHERE organization_id=$1 AND status='PENDING' AND available_at <= now()
ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`, organizationID).Scan(&id, &eventType, &payload)
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		if _, err = tx.Exec(ctx, "UPDATE outbox_events SET status='PROCESSING',attempts=attempts+1,locked_at=now() WHERE id=$1", id); err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			return
		}
		if err = d.process(ctx, organizationID, id, eventType, payload); err != nil {
			d.Logger.Error("outbox event failed", "event_id", id, "event_type", eventType, "error", err)
			_, _ = d.Store.Pool.Exec(ctx, `UPDATE outbox_events SET status=CASE WHEN attempts >= 5 THEN 'DLQ' ELSE 'PENDING' END,
available_at=now() + (LEAST(attempts,6) * interval '5 seconds'), last_error=$2, locked_at=NULL WHERE id=$1`, id, err.Error())
		}
	}
	d.scheduleEscalations(ctx, organizationID)
	d.deliverNotifications(ctx, organizationID)
}

func (d *Dispatcher) process(ctx context.Context, organizationID string, id int64, eventType string, raw []byte) error {
	switch eventType {
	case "webhook.received":
		var payload struct {
			DeliveryID string `json:"deliveryId"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil || payload.DeliveryID == "" {
			return fmt.Errorf("invalid webhook event payload")
		}
		if _, err := d.Store.Pool.Exec(ctx, `UPDATE webhook_deliveries SET status='PROCESSED',processed_at=now(),processing_error=NULL
WHERE id=$1 AND organization_id=$2 AND status IN ('ACCEPTED','PROCESSING','PROCESSED')`, payload.DeliveryID, organizationID); err != nil {
			return err
		}
	case "incident.created", "incident.ack", "incident.resolve":
		var payload struct {
			IncidentID string `json:"incidentId"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil || payload.IncidentID == "" {
			return fmt.Errorf("invalid incident event payload")
		}
		if eventType == "incident.created" {
			if err := d.scheduleIncidentRoutes(ctx, organizationID, payload.IncidentID); err != nil {
				return err
			}
		} else {
			if _, err := d.Store.Pool.Exec(ctx, `UPDATE incident_escalations
SET status='CANCELLED',cancelled_at=now()
WHERE organization_id=$1 AND incident_id=$2 AND status='SCHEDULED'`, organizationID, payload.IncidentID); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported event type %q", eventType)
	}
	_, err := d.Store.Pool.Exec(ctx, "UPDATE outbox_events SET status='DELIVERED',delivered_at=now(),locked_at=NULL,last_error=NULL WHERE id=$1", id)
	return err
}

func (d *Dispatcher) scheduleIncidentRoutes(ctx context.Context, organizationID, incidentID string) error {
	rows, err := d.Store.Pool.Query(ctx, `SELECT id::text,spec FROM alert_routes
WHERE organization_id=$1 AND enabled=true ORDER BY id`, organizationID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var routeID string
		var raw []byte
		if err := rows.Scan(&routeID, &raw); err != nil {
			return err
		}
		var route routeSpec
		if err := json.Unmarshal(raw, &route); err != nil || route.Channel != "webhook" || route.TargetRef == "" {
			return fmt.Errorf("route %s has invalid webhook specification", routeID)
		}
		if _, err := d.Store.Pool.Exec(ctx, `INSERT INTO notification_deliveries(organization_id,incident_id,route_id,delivery_key)
VALUES($1,$2,$3,'initial') ON CONFLICT DO NOTHING`, organizationID, incidentID, routeID); err != nil {
			return err
		}
		if route.Escalation == nil {
			continue
		}
		after, err := time.ParseDuration(route.Escalation.After)
		if err != nil || after < time.Minute || after > 7*24*time.Hour || route.Escalation.TargetRef == "" {
			return fmt.Errorf("route %s has invalid escalation", routeID)
		}
		if _, err := d.Store.Pool.Exec(ctx, `INSERT INTO incident_escalations(organization_id,incident_id,route_id,target_ref,scheduled_at)
VALUES($1,$2,$3,$4,now()+$5::interval) ON CONFLICT DO NOTHING`,
			organizationID, incidentID, routeID, route.Escalation.TargetRef, after.String()); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (d *Dispatcher) scheduleEscalations(ctx context.Context, organizationID string) {
	_, _ = d.Store.Pool.Exec(ctx, `UPDATE incident_escalations e SET status='CANCELLED',cancelled_at=now()
FROM incidents i WHERE e.organization_id=$1 AND e.incident_id=i.id AND i.status <> 'OPEN' AND e.status='SCHEDULED'`, organizationID)
	rows, err := d.Store.Pool.Query(ctx, `SELECT e.id::text,e.incident_id::text,e.route_id::text,e.target_ref
FROM incident_escalations e JOIN incidents i ON i.id=e.incident_id
WHERE e.organization_id=$1 AND e.status='SCHEDULED' AND e.scheduled_at <= now() AND i.status='OPEN'
ORDER BY e.scheduled_at FOR UPDATE SKIP LOCKED LIMIT 20`, organizationID)
	if err != nil {
		d.Logger.Error("incident escalation query failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var escalationID, incidentID, routeID, targetRef string
		if err := rows.Scan(&escalationID, &incidentID, &routeID, &targetRef); err != nil {
			continue
		}
		tx, err := d.Store.Pool.Begin(ctx)
		if err != nil {
			return
		}
		var claimed string
		err = tx.QueryRow(ctx, `UPDATE incident_escalations SET status='ESCALATED',escalated_at=now()
WHERE id=$1 AND organization_id=$2 AND status='SCHEDULED' RETURNING id::text`, escalationID, organizationID).Scan(&claimed)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO notification_deliveries(organization_id,incident_id,route_id,target_ref,delivery_key)
VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, organizationID, incidentID, routeID, targetRef, "escalation:"+escalationID)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			return
		}
	}
}

func (d *Dispatcher) deliverNotifications(ctx context.Context, organizationID string) {
	_, _ = d.Store.Pool.Exec(ctx, `UPDATE notification_deliveries SET status='PENDING',locked_at=NULL,available_at=now(),
last_error='worker interrupted while delivering notification' WHERE organization_id=$1 AND status='PROCESSING' AND locked_at < now() - interval '5 minutes'`, organizationID)
	rows, err := d.Store.Pool.Query(ctx, `SELECT n.id::text,ar.spec,coalesce(n.target_ref,''),i.id::text,i.title,i.severity,i.status,n.attempts
FROM notification_deliveries n JOIN alert_routes ar ON ar.id=n.route_id JOIN incidents i ON i.id=n.incident_id
WHERE n.organization_id=$1 AND n.status='PENDING' AND n.available_at <= now() ORDER BY n.created_at LIMIT 20`, organizationID)
	if err != nil {
		d.Logger.Error("notification queue query failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var deliveryID, targetRef, incidentID, title, severity, status string
		var spec []byte
		var attempts int
		if err := rows.Scan(&deliveryID, &spec, &targetRef, &incidentID, &title, &severity, &status, &attempts); err != nil {
			continue
		}
		var claimedAttempts int
		if err := d.Store.Pool.QueryRow(ctx, `UPDATE notification_deliveries SET status='PROCESSING',attempts=attempts+1,locked_at=now()
WHERE id=$1 AND status='PENDING' AND available_at <= now() RETURNING attempts`, deliveryID).Scan(&claimedAttempts); err != nil {
			continue
		}
		var route routeSpec
		if err := json.Unmarshal(spec, &route); err != nil || route.Channel != "webhook" || route.TargetRef == "" {
			d.blockDelivery(ctx, deliveryID, "rota não possui adaptador webhook válido")
			continue
		}
		if targetRef != "" {
			route.TargetRef = targetRef
		}
		endpoint := d.WebhookURLs[route.TargetRef]
		if endpoint == "" || !d.allowedEndpoint(endpoint) {
			d.blockDelivery(ctx, deliveryID, "destino de notificação não autorizado ou não configurado")
			continue
		}
		body, _ := json.Marshal(map[string]string{"incidentId": incidentID, "title": title, "severity": severity, "status": status, "deliveryId": deliveryID})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", deliveryID)
			req.Header.Set("User-Agent", "SentinelOps/notification")
		}
		client := d.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: 10 * time.Second}
		}
		if err == nil {
			resp, requestErr := client.Do(req)
			if requestErr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					_, _ = d.Store.Pool.Exec(ctx, "UPDATE notification_deliveries SET status='DELIVERED',delivered_at=now(),locked_at=NULL,last_error=NULL WHERE id=$1", deliveryID)
					continue
				}
				err = fmt.Errorf("notification endpoint returned HTTP %d", resp.StatusCode)
			} else {
				err = requestErr
			}
		}
		d.retryDelivery(ctx, deliveryID, claimedAttempts, err)
	}
}

func (d *Dispatcher) allowedEndpoint(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range d.AllowedHosts {
		if host == strings.ToLower(strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}
func (d *Dispatcher) blockDelivery(ctx context.Context, id, reason string) {
	_, _ = d.Store.Pool.Exec(ctx, "UPDATE notification_deliveries SET status='BLOCKED',locked_at=NULL,last_error=$2 WHERE id=$1", id, reason)
}
func (d *Dispatcher) retryDelivery(ctx context.Context, id string, attempts int, cause error) {
	reason := "notification request failed"
	if cause != nil {
		reason = cause.Error()
	}
	_, _ = d.Store.Pool.Exec(ctx, `UPDATE notification_deliveries SET status=CASE WHEN attempts >= 5 THEN 'DLQ' ELSE 'PENDING' END,locked_at=NULL,available_at=now() + (LEAST(attempts,6) * interval '5 seconds'),last_error=$2 WHERE id=$1`, id, reason)
}
