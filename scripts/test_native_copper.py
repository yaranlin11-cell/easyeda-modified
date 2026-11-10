import importlib.util,json,pathlib,tempfile,unittest,zipfile
from shapely.geometry import Polygon
spec=importlib.util.spec_from_file_location('native_copper',pathlib.Path(__file__).with_name('compose-native-reference-copper.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class NativeCopperTests(unittest.TestCase):
 def test_nonzero_paths_keep_a_fractured_hole(self):
  paths,r=m.copper_paths([(0,0),(4,0),(4,4),(0,4),(0,2),(1,2),(2,2),(2,1),(1,1),(1,2),(0,2)])
  self.assertEqual(r['holes'],1);self.assertEqual(r['areaMm2'],15);self.assertLess(r['serializationDifferenceMm2'],1e-7)
  def area(p):
   a=p[:2]+p[3:];pts=list(zip(a[::2],a[1::2]));return sum(x*yy-xx*y for (x,y),(xx,yy) in zip(pts,pts[1:]))/2
  self.assertGreater(area(paths[0]),0);self.assertLess(area(paths[1]),0)
 def test_real_crossing_is_refused(self):
  with self.assertRaisesRegex(ValueError,'normalization changes'):m.copper_paths([(0,0),(2,2),(0,2),(2,0)])
 def test_native_baseline_preserves_non_copper_and_rejects_wrong_target(self):
  def row(h,v):return json.dumps(h)+'||'+json.dumps(v)+'|\n'
  head=row({'type':'DOCHEAD'},{'docType':'PCB','uuid':'pcb'})
  layer=row({'type':'LAYER','id':'["LAYER",1]','ticket':1},{'use':True,'layerName':'TOP','layerType':'TOP'})
  component=row({'type':'COMPONENT','id':'part','ticket':2},{'zIndex':2,'opaqueUnchanged':{'footprint':'library'}})
  fill=row({'type':'FILL','id':'old','ticket':3},{'layerId':1,'netName':'N','zIndex':3,'path':[[0,0,'L',1,0,1,1,0,0]],'width':.2})
  other=row({'type':'DOCHEAD'},{'docType':'FOOTPRINT','uuid':'library'})
  reference=m.parse('(kicad_pcb (layers (0 "F.Cu" signal)) (zone (net "N") (filled_polygon (layer "F.Cu") (pts (xy 0 0) (xy 1 0) (xy 1 1) (xy 0 1)))))')
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'input.epro2'
   with zipfile.ZipFile(p,'w') as z:z.writestr('project.epru',head+layer+component+fill+other);z.writestr('asset.bin',b'unchanged')
   entries,r=m.compose(p,reference,'pcb')
   self.assertEqual(entries['asset.bin'],b'unchanged');self.assertIn(component,entries['project.epru'].decode());self.assertNotIn(fill,entries['project.epru'].decode());self.assertTrue(r['preservedRecordBytesVerified'])
   self.assertEqual(r['transportedCaches'],1);self.assertEqual(r['fillObjects'][0]['widthMil'],0)
   with self.assertRaisesRegex(ValueError,'missing or ambiguous'):m.compose(p,reference,'wrong')
if __name__=='__main__':unittest.main()
