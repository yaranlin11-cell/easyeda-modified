import json,tempfile,unittest,zipfile
from pathlib import Path
from native_contact_overlay import overlay
class NativeOverlay(unittest.TestCase):
 def run_case(self,bottom=False,changed=False,tombstone=False):
  ins={'x':10.,'y':20.,'angle':90,'layerId':2 if bottom else 1}
  q=dict(centerX=2,centerY=3,padAngle=0,padOffsetX=0,padOffsetY=0,specialPad=[],defaultPad={'padType':'RECT','width':4,'height':5,'radius':20},hole={'holeType':'ROUND','width':0,'height':0},plated=True)
  pad=dict(primitiveId='cp',x=13. if bottom else 7.,y=22.,layer=2 if bottom else 1)
  d=dict(capture={'projectUuid':'project'},components=[dict(primitiveId='c',x=10.,y=20.,rotation=90,availability={'pads':'available'},pads=[pad])],copper=dict(vias=[],fills=[],lines=[]))
  rows=[('DOCHEAD','doc',{'uuid':'doc'}),('COMPONENT','c',ins),('ATTR','a',{'parentId':'c','key':'Footprint','value':'fp'}),('DOCHEAD','fp',{'uuid':'fp','docType':'FOOTPRINT'}),('PAD','p',q)]
  if changed:d['components'][0]['x']+=1
  with tempfile.TemporaryDirectory() as td:
   p=Path(td)/'a.zip'
   with zipfile.ZipFile(p,'w') as z:z.writestr('data.epru','\n'.join(json.dumps({'type':t,'id':i})+'||'+json.dumps(v)+'|' for t,i,v in rows)+('\n'+json.dumps({'type':'LINE','id':'deleted'})+'|||' if tombstone else ''))
   return overlay(d,p,'doc',dict(context={'documentUuid':'doc','projectUuid':'project'},result={'dimensionSource':'native-document','vias':[]}))
 def test_top_transform(self):
  p=self.run_case()['components'][0]['pads'][0];self.assertAlmostEqual(p['x'],7);self.assertEqual(p['shape'],['RECT',4,5,20])
 def test_bottom_transform(self):self.assertAlmostEqual(self.run_case(True)['components'][0]['pads'][0]['x'],13)
 def test_changed_instance_rejected(self):
  with self.assertRaises(ValueError):self.run_case(changed=True)

 def test_unrelated_deleted_record(self):self.assertTrue(self.run_case(tombstone=True)["components"][0]["pads"][0]["nativeExact"])
