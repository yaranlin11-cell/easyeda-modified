"""Bounded, lossless-token S-expression utilities for offline reference audits.

These functions do not edit the live EDA and do not certify manufacturing fitness.
Quoted atoms remain quoted: electrical names must not be decoded or normalized.
"""
import collections
import hashlib
import re

TOKEN = re.compile(r'\s+|\(|\)|"(?:[^"\\]|\\.)*"|[^\s()"]+')


def parse(text):
    if len(text.encode('utf-8')) > 128 * 1024 * 1024:
        raise ValueError('board exceeds 128 MiB')
    stack = [[]]
    end = 0
    for match in TOKEN.finditer(text):
        if match.start() != end:
            raise ValueError('invalid or unterminated token')
        end = match.end()
        token = match[0]
        if token.isspace():
            continue
        if token == '(':
            if len(stack) >= 1024:
                raise ValueError('nesting limit')
            child = []
            stack[-1].append(child)
            stack.append(child)
        elif token == ')':
            if len(stack) == 1:
                raise ValueError('extra closing parenthesis')
            stack.pop()
        else:
            stack[-1].append(token)
    if end != len(text) or len(stack) != 1 or len(stack[0]) != 1:
        raise ValueError('truncated or multiple roots')
    board = stack[0][0]
    if not isinstance(board, list) or not board or board[0] != 'kicad_pcb':
        raise ValueError('expected kicad_pcb')
    return board


def emit(node):
    return '(' + ''.join(('\n' if isinstance(x,list) else (' ' if i else '')) + (emit(x) if isinstance(x,list) else x) for i,x in enumerate(node)) + ')'


def children(node, name):
    return [x for x in node if isinstance(x, list) and x and x[0] == name]


def one(node, name):
    found = children(node, name)
    if len(found) != 1:
        raise ValueError('expected exactly one ' + name)
    return found[0]


def walk(node):
    yield node
    for child in node:
        if isinstance(child, list):
            yield from walk(child)


def inventory(board):
    counts = collections.Counter(x[0] for x in board if isinstance(x, list) and x)
    pairs = collections.Counter(' '.join(one(v, 'layers')[1:]) for v in children(board, 'via'))
    pads = [p for f in children(board, 'footprint') for p in children(f, 'pad')]
    return {'objects': dict(counts), 'padCount': len(pads),
            'customPadCount': sum(p[3] == 'custom' for p in pads),
            'viaLayerPairs': dict(pairs),
            'boardThicknessMm': one(one(board, 'general'), 'thickness')[1],
            'filledPolygonPoints': sum(len(one(n, 'pts')) - 1 for n in walk(board) if n and n[0] == 'filled_polygon'),
            'stringOnlyNetReferences': sum(len(n) == 2 and n[1].startswith('"') for n in walk(board) if n and n[0] == 'net')}


def semantic_digest(board):
    """Ignore only generated object identities; keep all electrical/geometry tokens."""
    def cleaned(node):
        return [cleaned(x) if isinstance(x, list) else x for x in node
                if not (isinstance(x, list) and x and x[0] in ('uuid', 'tstamp'))]
    return hashlib.sha256(emit(cleaned(board)).encode()).hexdigest()


def legacy_net_references(board):
    """Encode KiCad 10 string-only references using the older numbered net table.

This is one field compatibility adapter, NOT a claim of full format downgrade.
"""
    if children(board, 'net'):
        raise ValueError('existing net table requires explicit reconciliation')
    refs = [(n, parent[0]) for parent in walk(board) for n in children(parent, 'net')]
    if any(len(n) != 2 or not n[1].startswith('"') for n, _ in refs):
        raise ValueError('mixed net-reference encoding')
    names = sorted({n[1] for n, _ in refs} - {'""'})
    mapping = {'""': '0', **{name: str(i+1) for i,name in enumerate(names)}}
    for parent in list(walk(board)):
        for ref in children(parent, 'net'):
            name = ref[1]
            ref[:] = ['net', mapping[name]] + ([name] if parent[0] == 'pad' else [])
            if parent[0] == 'zone':
                if children(parent, 'net_name'):
                    raise ValueError('unexpected zone net_name')
                parent.append(['net_name', name])
    insert_at = next((i for i,x in enumerate(board) if isinstance(x,list) and x and x[0] in ('footprint','via','segment','zone')), len(board))
    board[insert_at:insert_at] = [['net', ident, name] for name, ident in mapping.items()]
    return mapping


def rounded_rectangle_pad(pad, tolerance=0.000002):
    """Recognize exactly four quarter-circle corners; reject arbitrary custom pads.

KiCad arc midpoints are rounded in the source file. Recognition uses a 2 nm
coordinate tolerance; no other custom shapes are replaced.
"""
    import math
    if pad[0:1] != ['pad'] or len(pad) < 4 or pad[3] != 'custom':
        return False
    prims = one(pad, 'primitives')
    if len(prims) != 2 or prims[1][0] != 'gr_poly':
        return False
    poly = prims[1]
    if float(one(poly, 'width')[1]) != 0 or one(poly, 'fill')[1] != 'yes':
        return False
    points = one(poly, 'pts')[1:]
    if len(points) != 4 or any(p[0] != 'arc' for p in points):
        return False
    arcs = [[tuple(map(float,one(a,k)[1:])) for k in ('start','mid','end')] for a in points]
    ends = [p for a in arcs for p in (a[0],a[2])]
    hx,hy = max(abs(p[0]) for p in ends), max(abs(p[1]) for p in ends)
    radii = [math.dist(a[0],a[2])/math.sqrt(2) for a in arcs]
    r = round(sum(radii)/4,6)
    if r <= 0 or r > min(hx,hy) or any(abs(x-r)>tolerance for x in radii):
        return False
    corners = set()
    ordered_corners = []
    turn_signs = []
    for start,mid,end in arcs:
        sx = 1 if mid[0] > 0 else -1
        sy = 1 if mid[1] > 0 else -1
        c = (sx*(hx-r),sy*(hy-r)); corners.add((sx,sy))
        ordered_corners.append((sx,sy))
        cross = (start[0]-c[0])*(end[1]-c[1])-(start[1]-c[1])*(end[0]-c[0])
        turn_signs.append(1 if cross > 0 else -1)
        expected = {(round(sx*hx,6),round(sy*(hy-r),6)),(round(sx*(hx-r),6),round(sy*hy,6))}
        if {(round(start[0],6),round(start[1],6)),(round(end[0],6),round(end[1],6))} != expected:
            return False
        expected_mid = (c[0]+sx*r/math.sqrt(2),c[1]+sy*r/math.sqrt(2))
        if math.dist(mid,expected_mid) > tolerance:
            return False
    if len(corners) != 4 or len(set(turn_signs)) != 1 or any(abs(a[0]-b[0])+abs(a[1]-b[1]) != 2 for a,b in zip(ordered_corners,ordered_corners[1:]+ordered_corners[:1])):
        return False
    anchor = one(pad,'size')
    if max(map(float,anchor[1:]))/2 > min(hx,hy)-r:
        return False
    pad[3] = 'roundrect'
    anchor[:] = ['size', format(2*hx,'.9g'), format(2*hy,'.9g')]
    pad.remove(prims)
    pad.remove(one(pad,'options'))
    pad.append(['roundrect_rratio',format(r/(2*min(hx,hy)),'.12g')])
    return True


def legacy_footprint_placements(board):
    """Translate unit-scale KiCad 11 transforms into KiCad's older at field.

Non-unit scales, reflections and unrecognized transforms are refused.
    """
    planned = []
    for fp in children(board, 'footprint'):
        transforms = children(fp, 'transform')
        if not transforms:
            continue
        if len(transforms) != 1 or children(fp, 'at'):
            raise ValueError('ambiguous footprint placement')
        t = transforms[0]
        if {x[0] for x in t[1:] if isinstance(x,list)} != {'translate','rotate','scale'} or len(t) != 4:
            raise ValueError('unknown transform')
        scale = one(t,'scale')
        if scale[1:] != ['1','1']:
            raise ValueError('non-unit transform cannot be encoded as at')
        pos = one(t,'translate')[1:]
        angle = one(t,'rotate')[1:]
        if len(pos) != 2 or len(angle) != 1:
            raise ValueError('invalid transform')
        planned.append((fp,t,['at',*pos,*angle]))
    for fp,t,at in planned:
        fp[fp.index(t)] = at
    return len(planned)


def split_full_circle_keepouts(board):
    """Represent single start=end circular keepouts as four quarter arcs.

    An importer may treat the degenerate chord as an empty contour. This keeps
    true arcs (no chord approximation), no rule/net/layer edits, and no caches
    are removed. Coordinates are serialized to 12 decimals (mm).
    """
    import math
    converted = 0
    for zone in children(board, 'zone'):
        if not children(zone, 'keepout'):
            continue
        for polygon in children(zone, 'polygon'):
            pts = one(polygon, 'pts')
            if len(pts) != 2 or pts[1][0] != 'arc':
                continue
            arc = pts[1]
            if len(arc) != 4 or {x[0] for x in arc[1:]} != {'start', 'mid', 'end'}:
                raise ValueError('unsupported single arc keepout')
            start, mid, end = [tuple(map(float, one(arc, key)[1:])) for key in ('start', 'mid', 'end')]
            if any(len(p) != 2 or not all(map(math.isfinite, p)) for p in (start, mid, end)):
                raise ValueError('nonfinite circular keepout')
            if start != end:
                continue
            center = ((start[0]+mid[0])/2, (start[1]+mid[1])/2)
            radius = math.dist(start, mid)/2
            if radius <= 0:
                raise ValueError('zero radius circular keepout')
            theta = math.atan2(start[1]-center[1], start[0]-center[0])
            def point(k):
                if k in (0, 8): return start
                if k == 4: return mid
                a = theta+k*math.pi/4
                return center[0]+radius*math.cos(a),center[1]+radius*math.sin(a)
            def field(name,k):
                values = [format(v,'.12f') for v in point(k)]
                if abs(math.dist(tuple(map(float,values)),center)-radius) > 1e-10:
                    raise ValueError('circle serialization exceeded numeric tolerance')
                return [name,*values]
            pts[1:] = [['arc',field('start',2*i),field('mid',2*i+1),field('end',2*i+2)] for i in range(4)]
            converted += 1
    return converted
