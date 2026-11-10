"""Resolve fractured rings and partition filled copper without smoothing coordinates.
Requires Shapely 2.x. Results require source/target area and topology verification.
"""
import math
from shapely.geometry import Polygon, box
from shapely.validation import make_valid
from shapely.ops import unary_union


def polygon_parts(geometry):
    if geometry.geom_type == 'Polygon':
        return [geometry]
    if hasattr(geometry, 'geoms'):
        return [p for g in geometry.geoms for p in polygon_parts(g)]
    return []


def partition(points, max_area_error=1e-7):
    if not math.isfinite(max_area_error) or max_area_error <= 0:
        raise ValueError('area tolerance must be positive and finite')
    if len(points) < 3 or any(len(p) != 2 or not all(math.isfinite(v) for v in p) for p in points):
        raise ValueError('finite 2D polygon coordinates required')
    original = Polygon(points)
    valid = make_valid(original)
    source = unary_union(polygon_parts(valid))
    # Fracture bridge linework has zero area; never accept a changed fill area.
    if abs(source.area - original.area) > max_area_error:
        raise ValueError('fracture normalization changes filled area')
    pieces=[]
    for poly in polygon_parts(source):
        xmin,ymin,xmax,ymax=poly.bounds
        cuts=sorted({ymin,ymax,*[(Polygon(h).bounds[1]+Polygon(h).bounds[3])/2 for h in poly.interiors]})
        for low,high in zip(cuts,cuts[1:]):
            pieces.extend(polygon_parts(poly.intersection(box(xmin,low,xmax,high))))
    if any(p.interiors or not p.is_valid for p in pieces):
        raise ValueError('partition retains holes or invalid geometry')
    restored=unary_union(pieces)
    error=source.symmetric_difference(restored).area
    if error > max_area_error:
        raise ValueError('partition changes copper geometry')
    return pieces, {'sourceAreaMm2':source.area,'differenceAreaMm2':error,'pieces':len(pieces),
                    'maxPoints':max(len(p.exterior.coords) for p in pieces),'sourceValidAsSimpleRing':original.is_valid}
