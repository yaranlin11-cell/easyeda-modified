import importlib.util,pathlib,unittest
s=importlib.util.spec_from_file_location('drills',pathlib.Path(__file__).with_name('check-gerber-via-drills.py'));m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
class Drill(unittest.TestCase):
 def text(self,body):return 'M48\nMETRIC,LZ,0000.000000\nT01C0.07\n%\nG90\nT01\n'+body+'\nM30\n'
 def test_hit(self):self.assertEqual(m.parse_drill(self.text('X1.0Y-2.0')),[(1.,-2.,.07)])
 def test_slot_rejected(self):
  with self.assertRaises(ValueError):m.parse_drill(self.text('G85X1.0Y2.0'))
 def test_missing_eof(self):
  with self.assertRaises(ValueError):m.parse_drill(self.text('X1.0Y2.0').replace('M30',''))
 def test_undefined_tool(self):
  with self.assertRaises(ValueError):m.parse_drill(self.text('T02\nX1.0Y2.0'))
