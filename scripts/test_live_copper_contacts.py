import unittest
from live_copper_contacts import annulus,track,pad_shape,classify,fill_parts
class Contacts(unittest.TestCase):
 def pad(self,x=0,y=0,rotation=0):return pad_shape(dict(shape=['RECT',2,1],specialPadPresent=False,rotation=rotation,x=x,y=y))
 def test_width_contact_without_center_contact(self):self.assertEqual(classify(self.pad(),track(1.1,-2,1.1,2,.4)),'contact')
 def test_gap_not_bridged(self):self.assertEqual(classify(self.pad(),track(1.21,-2,1.21,2,.4)),'separate')
 def test_rotation(self):self.assertEqual(classify(self.pad(rotation=90),track(-2,.9,2,.9,.1)),'contact')
 def test_hole_is_not_copper(self):self.assertEqual(classify(annulus(0,0,4,2),track(-.1,0,.1,0,.1)),'separate')
 def test_ring_contact(self):self.assertEqual(classify(annulus(0,0,4,2),track(1.5,0,3,0,.1)),'contact')
 def test_exact_tangent_unresolved(self):self.assertEqual(classify(self.pad(),self.pad(x=2)),'unresolved-boundary')
 def test_bad_annulus(self):
  with self.assertRaises(ValueError):annulus(0,0,2,3)
 def test_fill_hole(self):
  f=dict(lineWidth=0,geometryAvailable=True,source=[[0,0,'L',4,0,4,4,0,4,0,0],[1,1,'L',1,3,3,3,3,1,1,1]])
  g=fill_parts(f)[0];self.assertEqual(classify((g,g),self.pad(x=2,y=2)),'unresolved-boundary')
if __name__=='__main__':unittest.main()

class CaptureRegression(unittest.TestCase):
 def test_native_via_and_pad_hole(self):
  from live_copper_contacts import extract
  pad=dict(primitiveId='p',shape=['ELLIPSE',4,4],specialPadPresent=False,rotation=0,x=0,y=0,layer=12,net='A',nativeExact=True,nativeHole={'holeType':'ROUND','width':2,'height':2},nativePlated=True)
  d=dict(componentsAvailability='available',components=[dict(primitiveId='c',availability={'pads':'available'},pads=[pad])],copper=dict(availability={k:'available' for k in ('routing','vias','fills','poured')},lines=[dict(primitiveId='t',layer=1,startX=-.1,startY=0,endX=.1,endY=0,lineWidth=.1,net='A')],arcs=[],vias=[],fills=[],poured=[]))
  rules={'rules':{'config':{'Physics':{'Blind/Buried Via':{'blindVia':{'table':[]}}}}}}
  layers={'copperLayerCount':2,'layers':[{'id':i,'type':'SIGNAL','layerStatus':1} for i in (1,2)]}
  g=extract(d,rules,layers)
  self.assertEqual([e['kind'] for e in g['edges']],['pad-barrel'])
  self.assertFalse(g['completeAcceptance'])

class RoundedPads(unittest.TestCase):
 def test_corner_not_copper(self):
  p=pad_shape(dict(shape=['RECT',2,2,50],specialPadPresent=False,rotation=0,x=0,y=0))
  self.assertEqual(classify(p,track(.98,.98,.99,.99,.001)),'separate')
 def test_radius_area(self):
  import math
  p=pad_shape(dict(shape=['RECT',2,2,50],specialPadPresent=False,rotation=0,x=0,y=0))
  area=4-(4-math.pi)*.5**2
  self.assertLess(p[0].area,area);self.assertGreater(p[1].area,area)

class OffsetBounds(unittest.TestCase):
 def test_offset_uncertainty_not_silently_ignored(self):
  p=pad_shape(dict(shape=['RECT',2,2],specialPadPresent=False,rotation=0,x=0,y=0,positionUncertaintyMil=.1))
  self.assertEqual(classify(p,track(.95,-.1,.95,.1,.01)),'unresolved-boundary')
