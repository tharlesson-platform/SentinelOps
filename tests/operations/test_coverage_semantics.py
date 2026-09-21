from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[2]/"scripts"))
import unittest
from coverage_semantics import verify_topk,trace_query,ensure_topk_basis
class SemanticsTest(unittest.TestCase):
 def rows(self,values):return [[[["id",str(i)]],[[1,v]]] for i,v in enumerate(values)]
 def test_tie_is_valid_but_corruption_is_not(self):
  base=self.rows([4,3,3,2]);a=base[:2];self.assertTrue(verify_topk(a,base,2,False)['passed']);a[1][1][0][1]=3.0000000001;self.assertFalse(verify_topk(a,self.rows([4,3,3,2]),2,False)['passed'])
 def test_missing_winner_and_outside_rank_rejected(self):
  b=self.rows([4,3,3,2]);self.assertFalse(verify_topk(b[1:3],b,2,False)['passed']);self.assertFalse(verify_topk([b[0],b[3]],b,2,False)['passed']);self.assertFalse(verify_topk([b[0]],b,2,False)['passed'])
 def test_labels_timestamp_and_limit_rejected(self):
  b=self.rows([1]);a=self.rows([1]);a[0][1][0][0]=2;self.assertFalse(verify_topk(a,b,1,False)['passed']);self.assertFalse(verify_topk(b,b,1,True)['passed'])
 def test_truncated_retained_series_cannot_lose_strict_winner(self):
  b=[[[['id',str(i)]],[[i,10]]] for i in range(41)];a=b[:40];self.assertTrue(verify_topk(a,b,10,True)['passed'])
  a[0]=[a[0][0],[]];self.assertFalse(verify_topk(a,b,10,True)['passed'])

 def test_infinite_basis_is_rejected_before_ranking(self):
  for value in ['+Inf','-Inf']:
   with self.assertRaises(ValueError):ensure_topk_basis({'status':'success','data':{'resultType':'matrix','result':[{'values':[[1,value]]}]}})
  ensure_topk_basis({'status':'success','data':{'resultType':'matrix','result':[{'values':[[1,'NaN'],[2,'1']]}]}})

 def test_trace_query_keeps_filter_and_all_ids(self):
  q=trace_query('{ resource.service.name = "orders" && status = error }',['ab','cd']);self.assertIn('status = error',q);self.assertIn('trace:id = "ab" || trace:id = "cd"',q)
  with self.assertRaises(ValueError):trace_query('{}',['x" || true'])
if __name__=='__main__':unittest.main()
