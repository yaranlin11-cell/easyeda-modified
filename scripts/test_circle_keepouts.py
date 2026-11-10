import copy, math, unittest
from kicad_reference import split_full_circle_keepouts, children, one
class CircleKeepouts(unittest.TestCase):
 def board(self):
  return ['kicad_pcb',['zone',['layers','"F.Cu"','"In1.Cu"'],['keepout',['tracks','not_allowed'],['vias','allowed']],['polygon',['pts',['arc',['start','3','1'],['mid','-1','1'],['end','3','1']]]]]]
 def test_circle_is_exactly_four_arcs_and_rules_layers_preserved(self):
  b=self.board();before=copy.deepcopy(b);self.assertEqual(split_full_circle_keepouts(b),1)
  z=children(b,'zone')[0];self.assertEqual(z[:3],before[1][:3]);pts=one(one(z,'polygon'),'pts')[1:]
  self.assertEqual(len(pts),4)
  for i,arc in enumerate(pts):
   self.assertEqual(one(arc,'end')[1:],one(pts[(i+1)%4],'start')[1:])
   for name in ('start','mid','end'):
    xy=list(map(float,one(arc,name)[1:]));self.assertAlmostEqual(math.dist(xy,(1,1)),2,places=10)
  self.assertEqual(split_full_circle_keepouts(b),0)
 def test_nonclosed_arc_is_not_invented_into_a_circle(self):
  b=self.board();one(one(one(b[1],'polygon'),'pts')[1],'end')[1]='2';before=copy.deepcopy(b)
  self.assertEqual(split_full_circle_keepouts(b),0);self.assertEqual(b,before)
 def test_zero_radius_rejected(self):
  b=self.board();one(one(one(b[1],'polygon'),'pts')[1],'mid')[1]='3'
  with self.assertRaisesRegex(ValueError,'zero radius'):split_full_circle_keepouts(b)
if __name__=='__main__':unittest.main()
