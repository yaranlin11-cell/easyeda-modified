import copy
import importlib.util
from pathlib import Path
import unittest
from kicad_reference import parse, emit, inventory, children, one, legacy_net_references, rounded_rectangle_pad, legacy_footprint_placements
spec = importlib.util.spec_from_file_location('prepare',Path(__file__).with_name('prepare-kicad-reference.py'))
prepare = importlib.util.module_from_spec(spec); spec.loader.exec_module(prepare)

BASE = '(kicad_pcb (version 20260206) (general (thickness 1.6)) (layers (0 "F.Cu" signal) (2 "B.Cu" signal)) (setup))'
class ReferenceTests(unittest.TestCase):
    def test_roundtrip_quotes_and_boundary(self):
        b=parse(BASE[:-1]+' (property "x" "a \\\"(b)\\\" c"))')
        self.assertEqual(parse(emit(b)),b)
        self.assertLess(max(map(len,emit(b).splitlines())),200)
        for s in ('(kicad_pcb', '(kicad_pcb) ()','(kicad_pcb "unterminated)'):
            with self.assertRaises(ValueError):parse(s)
    def recipe(self):
        return dict(provenanceKind='requirement',locator='p1',copperUm=[35,35],dielectricUm=[504],maskUm=[13,13],finishedThicknessUm=600,dielectricTypes=['core'],dielectricMaterials=['explicit material'],maskMaterial='explicit mask')
    def test_stackup_sum_provenance_and_nonoverwrite(self):
        b=parse(BASE);prepare.apply_stackup(b,self.recipe())
        self.assertEqual(one(one(b,'general'),'thickness')[1],'0.6')
        self.assertEqual(one(one(one(b,'setup'),'stackup')[3],'type')[1],'"core"')
        with self.assertRaises(ValueError):prepare.apply_stackup(b,self.recipe())
        for bad in ({'finishedThicknessUm':601},{'provenanceKind':'native-observation'},{'dielectricUm':['NaN']},{'dielectricTypes':[]}):
            r=self.recipe();r.update(bad)
            with self.assertRaises(ValueError):prepare.apply_stackup(parse(BASE),r)
    def test_net_table_precedes_uses_and_names_preserved(self):
        b=parse(BASE[:-1]+' (footprint "f" (pad "1" smd rect (net "A\\\"B"))) (segment (net "A\\\"B")) (zone (net "GND")))')
        m=legacy_net_references(b)
        self.assertEqual(one(children(b,'footprint')[0][2],'net')[1:], [m['"A\\\"B"'],'"A\\\"B"'])
        self.assertLess(b.index(children(b,'net')[0]),b.index(children(b,'footprint')[0]))
        self.assertEqual(one(children(b,'zone')[0],'net_name')[1],'"GND"')
        with self.assertRaises(ValueError):legacy_net_references(b)
    def test_custom_shape_not_silently_replaced(self):
        b=parse(BASE[:-1]+' (footprint "f" (pad "1" smd custom (size .1 .1) (primitives (gr_poly (pts (xy 0 0) (xy 1 0) (xy 1 1)) (width 0) (fill yes))))))')
        pad=children(children(b,'footprint')[0],'pad')[0];before=copy.deepcopy(pad)
        self.assertFalse(rounded_rectangle_pad(pad));self.assertEqual(pad,before)
    def test_transform_preserves_placement_and_rejects_scale(self):
        b=parse(BASE[:-1]+' (footprint "f" (transform (translate 12 34) (rotate 90) (scale 1 1))))')
        self.assertEqual(legacy_footprint_placements(b),1)
        self.assertEqual(one(children(b,'footprint')[0],'at'),['at','12','34','90'])
        b=parse(BASE[:-1]+' (footprint "f" (transform (translate 12 34) (rotate 90) (scale -1 1))))')
        with self.assertRaises(ValueError):legacy_footprint_placements(b)

    def test_roundrect_and_malformed_corner_order(self):
        p=['pad','"1"','smd','custom',['size','.075','.075'],['options',['clearance','outline'],['anchor','circle']],['primitives',['gr_poly',['pts',
            ['arc',['start','-.125','.15'],['mid','-.142678','.142678'],['end','-.15','.125']],
            ['arc',['start','-.15','-.125'],['mid','-.142678','-.142678'],['end','-.125','-.15']],
            ['arc',['start','.125','-.15'],['mid','.142678','-.142678'],['end','.15','-.125']],
            ['arc',['start','.15','.125'],['mid','.142678','.142678'],['end','.125','.15']]],['width','0'],['fill','yes']]]]
        bad=copy.deepcopy(p);pts=one(one(bad,'primitives')[1],'pts');pts[2],pts[3]=pts[3],pts[2]
        self.assertFalse(rounded_rectangle_pad(bad))
        self.assertTrue(rounded_rectangle_pad(p));self.assertEqual(one(p,'size')[1:],['0.3','0.3'])
        self.assertAlmostEqual(float(one(p,'roundrect_rratio')[1]),1/12)

    def test_live_net_loss_rejected(self):
        sp=importlib.util.spec_from_file_location('check',Path(__file__).with_name('check-kicad-live-reference.py'))
        check=importlib.util.module_from_spec(sp);sp.loader.exec_module(check)
        src={'components':[{'ref':'R1','pads':[{'number':'1','net':'GND','xMm':25.4,'yMm':25.4}]}],'padCount':1,'viaCount':0,'trackPrimitives':0}
        live={'componentsAvailability':'available','components':[{'designator':'R1','availability':{'pads':'available'},'pads':[{'padNumber':'1','net':'GND','x':1000,'y':-1000}]}],'copper':{'availability':{'routing':'available','vias':'available'}}}
        self.assertEqual(check.compare(src,live)['status'],'partial-check-passed')
        live['components'][0]['pads'][0]['net']=''
        self.assertEqual(check.compare(src,live)['status'],'failed')
        self.assertFalse(check.compare(src,live)['completeAcceptance'])
if __name__ == '__main__':unittest.main()

