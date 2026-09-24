import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch,Mock
import sys,json
sys.path.insert(0,str(Path(__file__).resolve().parents[2]/'scripts'))
import deploy_observability_release as release
from api_edge_drain import DrainTimeout

def item(number,image):
 return {'Id':str(number),'Name':'api-'+str(number),'Image':image,'State':{'Status':'running','Health':{'Status':'healthy'}},'NetworkSettings':{'Networks':{'sentinelops_control':{'IPAddress':'172.20.0.'+str(number)},'sentinelops_telemetry':{}}},'RestartCount':0}
class ReleaseTests(unittest.TestCase):
 def test_bundle_rejects_root_assets_and_accepts_deployed_subpath(self):
  with tempfile.TemporaryDirectory() as tmp:
   r=Path(tmp);(r/'web/assets').mkdir(parents=True);(r/'web/assets/app.js').write_text('fixture')
   with patch.object(release,'RELEASE',r):
    (r/'web/index.html').write_text('<script src="/assets/app.js"></script><script src="/config.js"></script>')
    with self.assertRaises(AssertionError):release.validate_web_bundle()
    (r/'web/index.html').write_text('<script src="/sentinelops/assets/app.js"></script><script src="/sentinelops/config.js"></script>')
    release.validate_web_bundle()

 def test_dns_uses_full_a_answer_without_resolver_address(self):
  with patch.object(release,'run',return_value='Server: 127.0.0.11\nAddress: 127.0.0.11:53\nName: api\nAddress: 172.18.0.7\nName: api\nAddress: 172.18.0.16\n'):
   self.assertEqual(release.resolved_api_ips(),{'172.18.0.7','172.18.0.16'})

 def test_probe_rejects_spa_200_and_requires_api_health(self):
  with patch.object(release,'get',return_value=b'<html>SPA</html>'):
   with self.assertRaises(json.JSONDecodeError):release.probe()
  with patch.object(release,'get',return_value=b'{"data":{"status":"ok","time":"2026-09-21T12:00:00Z"}}') as get:
   release.probe();get.assert_called_once_with(release.BASE+'/healthz')

 def test_forward_and_rollback_pin_before_scale_and_drain_before_stop(self):
  current=[item(1,'old'),item(2,'old')];state={'dynamic':True,'target':'new','serial':2,'eligible':set()};events=[]
  class Edge:
   admitted={'172.20.0.1','172.20.0.2'}
   def pin(self,ips):self.admitted=set(ips);state.update(dynamic=False,eligible=set(ips));events.append('drain')
   def restore_dynamic(self):state['dynamic']=True;events.append('dynamic')
  def scale():
   self.assertFalse(state['dynamic'],'new peer advertised before readiness');self.assertLess(len(current),3);state['serial']+=1;current.append(item(state['serial'],state['target']));events.append('scale')
  def remove(x):
   self.assertNotIn(release.ip(x),state['eligible'],'stopped peer still eligible');current.remove(x);events.append('remove')
  def healthy(image,count,guard):
   got=[x for x in current if x['Image']==image];self.assertGreaterEqual(len(got),count);return got
  with patch.object(release,'apis',lambda:list(current)),patch.object(release,'scale3',scale),patch.object(release,'remove',remove),patch.object(release,'wait_healthy',healthy),patch.object(release,'read_json',lambda p:{'new_image':'candidate-disabled-for-unit-test'}),patch.object(release,'run',lambda args:'\n'.join('Address: '+release.ip(x) for x in current)),patch.object(release,'probe'),patch.object(release,'auth'):
   edge=Edge();release.replace_replicas('new',edge,lambda:None)
   self.assertEqual({x['Image'] for x in current},{'new'});self.assertTrue(state['dynamic'])
   # Failure after DNS restoration: rollback must pin anew before creating old peers.
   events.clear();state['target']='old';release.replace_replicas('old',edge,lambda:None)
   self.assertEqual(events[0],'drain');self.assertEqual({x['Image'] for x in current},{'old'});self.assertEqual(len(current),2);self.assertTrue(state['dynamic'])
 def test_rejected_candidate_is_never_admitted_during_rollback(self):
  current=[item(1,'old'),item(2,'old'),item(3,'rejected')];seen=[]
  class Edge:
   admitted={'172.20.0.1','172.20.0.2'}
   def pin(self,ips):seen.extend(ips);self.admitted=set(ips)
   def restore_dynamic(self):pass
  with patch.object(release,'apis',lambda:list(current)),patch.object(release,'remove',lambda x:current.remove(x)),patch.object(release,'wait_healthy',lambda image,count,guard:[x for x in current if x['Image']==image]),patch.object(release,'run',lambda args:'\n'.join('Address: '+release.ip(x) for x in current)),patch.object(release,'probe'),patch.object(release,'auth'),patch.object(release,'scale3') as scale:
   release.replace_replicas('old',Edge(),lambda:None)
   self.assertNotIn('172.20.0.3',seen);scale.assert_not_called();self.assertEqual([x['Id'] for x in current],['1','2'])
 def initial_failure(self,error):
  with tempfile.TemporaryDirectory() as tmp:
   root=Path(tmp);r=root/'release';b=r/'backup';web=root/'web';b.mkdir(parents=True);web.mkdir();(b/'web').mkdir()
   for p,data in [(root/'.env',b'fixture'),(root/'DEPLOYED_COMMIT',b'base'),(b/'DEPLOYED_COMMIT',b'base'),(root/'lock',b'lock'),(b/'images.lock.yml',b'lock'),(root/'edge',b'edge'),(b/'edge.conf',b'edge')]:p.write_bytes(data)
   for name in ['index.html','config.js']:(web/name).write_text(name);(b/'web'/name).write_text(name)
   prepared={'base_image':'old','new_image':'new','environment_sha256':release.sha(root/'.env')}
   (r/'prepared.json').write_text(json.dumps(prepared));(r/'before-api.json').write_text(json.dumps([{'id':'1'},{'id':'2'}]))
   (r/'before-runtime.json').write_text(json.dumps({'checks':{'fixture':{'passed':True}}}))
   edge=Mock();edge.pin.side_effect=error
   with patch.object(release,'validate_web_bundle'),patch.multiple(release,ROOT=root,RELEASE=r,BACKUP=b,WEB=web,LOCK=root/'lock',EDGE_CONFIG=root/'edge'),patch.object(release,'apis',return_value=[item(1,'old'),item(2,'old')]),patch.object(release,'probe'),patch.object(release,'auth'),patch.object(release,'EdgeDrainer',return_value=edge),patch.object(release,'replace_replicas') as replace,patch.object(release,'set_image') as set_image:
    with self.assertRaises(type(error)):release.deploy()
    replace.assert_not_called();set_image.assert_not_called()
    return edge.restore_dynamic.call_count
 def test_failure_during_first_pin_recovers_only_edge(self):self.assertEqual(self.initial_failure(RuntimeError('probe failed after reload')),1)
 def test_drain_timeout_keeps_every_replica_alive(self):self.assertEqual(self.initial_failure(DrainTimeout('long connection')),0)
if __name__=='__main__':unittest.main()
