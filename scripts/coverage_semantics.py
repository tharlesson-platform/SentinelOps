"""Equivalência de resultados limitados, sem tolerância numérica nem relaxamento de filtros."""
import math

def verify_topk(actual,basis,k,truncated):
    # Canonical rows: [[sorted labels], [[timestamp, finite value], ...]].
    source={tuple(tuple(pair) for pair in labels):dict(points) for labels,points in basis}
    if len(source)!=len(basis):raise ValueError('duplicate source labels')
    by_time={}
    for label,points in source.items():
        for ts,value in points.items():by_time.setdefault(ts,{})[label]=value
    seen={};errors=[]
    retained={tuple(tuple(pair) for pair in labels) for labels,points in actual}
    if len(actual)>40:errors.append('series limit exceeded')
    if len({tuple(tuple(pair) for pair in x[0]) for x in actual})!=len(actual):errors.append('duplicate output labels')
    for labels,points in actual:
        label=tuple(tuple(pair) for pair in labels)
        if label not in source:errors.append('unknown labels');continue
        if len(points)!=len(dict(points)):errors.append('duplicate timestamp')
        for ts,value in points:
            seen.setdefault(ts,{})[label]=value
            if source[label].get(ts)!=value:errors.append('value or timestamp mismatch');continue
            if sum(v>value for v in by_time[ts].values())>=k:errors.append('point outside topk')
    for ts,values in by_time.items():
        got=seen.get(ts,{})
        if len(got)>k:errors.append('too many winners')
        ordered=sorted(values.values(),reverse=True)
        cutoff=ordered[min(k,len(ordered))-1] if ordered else math.inf
        required={key for key,value in values.items() if value>cutoff or len(values)<=k}
        if truncated:required &= retained
        elif len(got)!=min(k,len(values)):errors.append('missing topk points')
        if not required.issubset(got):errors.append('missing strict winner')
    if truncated and not any(points for labels,points in actual):errors.append('empty truncated result')
    if truncated and len(actual)!=40:errors.append('unexplained truncation')
    return {'passed':not errors,'method':'topk_exact_values_and_rank','checkedSeries':len(actual),'checkedPoints':sum(len(x[1]) for x in actual),'basisSeries':len(basis),'truncated':truncated,'errors':sorted(set(errors))}

def trace_query(original,ids):
    import re
    if not original.startswith('{') or not original.endswith('}') or original.count('{')!=1:raise ValueError('unsupported trace template')
    if not ids or len(ids)>50 or len(set(ids))!=len(ids):raise ValueError('invalid trace set')
    if not all(re.fullmatch('[0-9a-fA-F]{1,32}',x) for x in ids):raise ValueError('invalid trace ID')
    return original[:-1]+' && ('+' || '.join('trace:id = "'+x+'"' for x in ids)+') }'
