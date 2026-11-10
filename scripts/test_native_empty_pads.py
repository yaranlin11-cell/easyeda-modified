import unittest,tempfile,json,zipfile
from pathlib import Path
from native_empty_pads import empty_pad_proofs
class NativeEmptyTests(unittest.TestCase):
 def proof(self,pad=False,library=True,x=1):
  with tempfile.TemporaryDirectory() as d:
   p=Path(d)/'a.epro2';rows=[]
   def add(t,i,v):rows.append(json.dumps(dict(type=t,id=i))+'||'+json.dumps(v)+'|')
   add('DOCHEAD','b',dict(uuid='b',docType='PCB'));add('COMPONENT','c',dict(x=1,y=2,angle=0));add('ATTR','a',dict(parentId='c',key='Footprint',value='f'))
   if library:
    add('DOCHEAD','f',dict(uuid='f',docType='FOOTPRINT'));add('POLY','g',{})
    if pad:add('PAD','p',{})
   with zipfile.ZipFile(p,'w') as z:z.writestr('a.epru','\n'.join(rows))
   return empty_pad_proofs(p,'b',[dict(primitiveId='c',x=x,y=2,rotation=0)])
 def test_populated_graphic_library_proves_empty(self):self.assertEqual(self.proof()['c']['padCount'],0)
 def test_real_pad_not_hidden(self):self.assertEqual(self.proof(pad=True),{})
 def test_missing_library_not_empty(self):self.assertEqual(self.proof(library=False),{})
 def test_wrong_anchor_not_accepted(self):self.assertEqual(self.proof(x=3),{})
if __name__=='__main__':unittest.main()
