#!/usr/bin/env python3
"""Compare KiCad reference vias with identity-bound native EasyEDA via readback.

Read-only; checks individual net, layer pair, location and dimensions. Units mm.
This does not prove copper connectivity, DRC or manufacturing acceptance.
"""
import argparse, collections, hashlib, json, math
from pathlib import Path
from kicad_reference import parse, children, one


def compare(expected, actual, tolerance_mm=0.000003):
    if not math.isfinite(tolerance_mm) or not 0 < tolerance_mm <= 0.00001:
        raise ValueError('tolerance must be finite, positive and <= 10 nm')
    groups = collections.defaultdict(list)
    for row in actual:
        if not all(math.isfinite(row[k]) for k in ('x','y','hole','diameter')):
            raise ValueError('nonfinite geometry')
        groups[(row['net'], tuple(row['pair']))].append(row)
    errors, matched, max_position, max_dimension = [], 0, 0, 0
    for row in expected:
        key = (row['net'], tuple(row['pair']))
        candidates = groups[key]
        near = [v for v in candidates if math.hypot(v['x']-row['x'], v['y']-row['y']) <= tolerance_mm]
        if len(near) != 1:
            errors.append({'type':'missing-or-ambiguous-via','expected':row,'candidates':len(near)})
            continue
        found = near[0]; candidates.remove(found)
        dp = math.hypot(found['x']-row['x'], found['y']-row['y'])
        dd = max(abs(found[k]-row[k]) for k in ('hole','diameter'))
        max_position, max_dimension = max(max_position,dp), max(max_dimension,dd)
        if dd > tolerance_mm: errors.append({'type':'dimension-mismatch','expected':row,'actual':found})
        else: matched += 1
    for rows in groups.values():
        errors.extend({'type':'unexpected-via','actual':row} for row in rows)
    return dict(status='passed' if not errors else 'failed', expectedCount=len(expected), observedCount=len(actual),
                matched=matched, toleranceMm=tolerance_mm, maxPositionErrorMm=max_position,
                maxDimensionErrorMm=max_dimension, errors=errors, completeAcceptance=False,
                unverified=['copper connectivity','padstack shapes on individual layers','DRC','manufacturing acceptance'])


def decoded(value):
    return json.loads(value) if value.startswith('"') else value


def load_inputs(board, native, rules, layers):
    if native.get('ok') is not True or native['result'].get('dimensionSource') != 'native-document':
        raise ValueError('native dimension source required; SDK rounded readings are not manufacturing evidence')
    target = native['context']
    if any(not isinstance(target.get(k),str) or not target[k] for k in ('projectUuid','documentUuid')):
        raise ValueError('exact project/document identity required')
    for document in (rules,layers):
        if document.get('ok') is not True or any(document['context'].get(k) != target.get(k) for k in ('projectUuid','documentUuid')):
            raise ValueError('readbacks target different documents')
    live_layers = layers['result']['layers']
    mapping = {}
    for layer in one(board,'layers')[1:]:
        name = decoded(layer[1])
        if not name.endswith('.Cu'): continue
        if name == 'F.Cu': candidates = [v for v in live_layers if v['id']==1 and v['type']=='SIGNAL']
        elif name == 'B.Cu': candidates = [v for v in live_layers if v['id']==2 and v['type']=='SIGNAL']
        else: candidates = [v for v in live_layers if len(layer)>3 and v['name']==decoded(layer[3]) and v['type'] in ('SIGNAL','PLANE')]
        if len(candidates)!=1: raise ValueError('layer mapping is missing or ambiguous: '+name)
        mapping[name]=candidates[0]['id']
    if len(mapping)!=layers['result']['copperLayerCount']: raise ValueError('copper layer count mismatch')
    netmap = {n[1]:decoded(n[2]) for n in children(board,'net') if len(n)==3}
    expected=[]
    for v in children(board,'via'):
        at=one(v,'at'); net=one(v,'net')[1]
        expected.append(dict(net=decoded(net) if net.startswith('"') else netmap[net],
            pair=[mapping[decoded(k)] for k in one(v,'layers')[1:]],x=float(at[1]),y=-float(at[2]),
            hole=float(one(v,'drill')[1]),diameter=float(one(v,'size')[1])))
    table=rules['result']['rules']['config']['Physics']['Blind/Buried Via']
    pairs={}
    for group in table.values():
        for row in group['table']:
            if row['name'] in pairs: raise ValueError('duplicate blind rule name')
            pairs[row['name']]=[row['startLayer'],row['endLayer']]
    actual=[]
    ids=set()
    for v in native['result']['vias']:
        if v['primitiveId'] in ids: raise ValueError('duplicate via identity')
        ids.add(v['primitiveId'])
        if v['viaType']==0: pair=[1,2]
        elif v['viaType']==1: pair=pairs[v['designRuleBlindViaName']]
        else: raise ValueError('unsupported via type')
        actual.append(dict(id=v['primitiveId'],net=v['net'],pair=pair,x=v['x']*.0254,y=v['y']*.0254,
                           hole=v['holeDiameter']*.0254,diameter=v['diameter']*.0254))
    return expected,actual


def main():
    p=argparse.ArgumentParser(description=__doc__)
    for arg in ('board','native','rules','layers','out'):p.add_argument('--'+arg,type=Path,required=True)
    args=p.parse_args(); paths=[args.board,args.native,args.rules,args.layers]
    expected,actual=load_inputs(parse(args.board.read_text()),*[json.loads(v.read_text()) for v in paths[1:]])
    result=compare(expected,actual)
    result['sources']=[dict(path=str(v),sha256=hashlib.sha256(v.read_bytes()).hexdigest()) for v in paths]
    with args.out.open('x') as f:json.dump(result,f,ensure_ascii=False,indent=2)
    print(result['status'],result['matched'],'/',result['expectedCount'],'vias; completeAcceptance=false')
    return int(result['status']!='passed')
if __name__=='__main__':raise SystemExit(main())
