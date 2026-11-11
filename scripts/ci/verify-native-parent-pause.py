#!/usr/bin/env python3
"""Real isolated traffic with only an owned test supervisor deliberately paused.
Run inside an already-created private network namespace as root. No mounts,
services, customer databases, externally routed traffic or production PIDs.
"""
import argparse,base64,hashlib,http.client,json,os,pwd,shutil,signal,socket,subprocess,tempfile,threading,time
from pathlib import Path
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer

def main():
 a=argparse.ArgumentParser();a.add_argument('--test',required=True);a.add_argument('--core',required=True);a.add_argument('--core-sha',required=True);a.add_argument('--output',required=True);a.add_argument('--seconds',type=int,choices=[15,30],default=30);a.add_argument('--pending',action='store_true');a.add_argument('--pause-native',action='store_true');x=a.parse_args()
 assert os.geteuid()==0,'private namespace/root launcher required'
 assert os.readlink('/proc/self/ns/net')!=os.readlink('/proc/1/ns/net'),'must run in a separate network namespace'
 subprocess.run(['ip','link','set','lo','up'],check=True)
 output=Path(x.output).resolve();assert not output.exists(),'refuse to replace prior evidence';output.mkdir(parents=True,mode=0o700)
 core=Path(x.core).resolve();test=Path(x.test).resolve();assert core.is_file() and test.is_file();assert hashlib.file_digest(open(core,'rb'),'sha256').hexdigest()==x.core_sha
 task=Path(tempfile.mkdtemp(prefix='dxp-pause-'));task.chmod(0o755);u=pwd.getpwnam('nobody')
 for name,src in [('runtime.test',test),('core',core)]:shutil.copyfile(src,task/name);(task/name).chmod(0o755)
 control=task/'control';scratch=task/'tmp'
 for d in [control,scratch]:d.mkdir(mode=0o700);os.chown(d,u.pw_uid,u.pw_gid)
 payload=b'independent-native-lease-proof';hits=[0]
 class Handler(BaseHTTPRequestHandler):
  def do_GET(self):
   hits[0]+=1;self.send_response(200);self.send_header('Content-Length',str(len(payload)));self.end_headers();self.wfile.write(payload)
  def log_message(self,*args):pass
 server=ThreadingHTTPServer(('127.0.0.1',0),Handler);threading.Thread(target=server.serve_forever,daemon=True).start()
 log=open(output/'test.log','w');env={'PATH':'/usr/bin:/bin','TMPDIR':str(scratch),'DARK_PRO_PARENT_PAUSE_CONTROL':str(control),'DARK_PRO_PARENT_PAUSE_LEASE_SECONDS':str(x.seconds),'DARK_PRO_PARENT_PAUSE_PENDING':'1' if x.pending else '0','DARK_PRO_EXPECT_AUTONOMOUS_LEASE':'1','DARK_PRO_TEST_INBOUND_CORE':str(task/'core'),'DXP_INBOUND_CORE_V2_SHA256':x.core_sha}
 process=subprocess.Popen([str(task/'runtime.test'),'-test.run=^TestOperatorParentPauseLease$','-test.v','-test.timeout=90s'],env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True,user=u.pw_uid,group=u.pw_gid,extra_groups=[])
 paused=set();ready=None
 try:
  end=time.monotonic()+20
  while not (control/'ready.json').exists():
   if process.poll() is not None:raise RuntimeError('fixture failed before readiness')
   if time.monotonic()>end:raise TimeoutError('fixture readiness')
   time.sleep(.03)
  ready=json.loads((control/'ready.json').read_text());pid=ready['pid'];native=ready['native_pid'];assert pid==process.pid and pid!=os.getpid()
  assert Path('/proc/'+str(pid)+'/exe').resolve()==task/'runtime.test';assert os.stat('/proc/'+str(pid)).st_uid==u.pw_uid
  assert Path('/proc/'+str(native)+'/exe').resolve()==task/'core';assert os.stat('/proc/'+str(native)).st_uid==u.pw_uid
  status=Path('/proc/'+str(native)+'/status').read_text();assert 'PPid:\t'+str(pid)+'\n' in status
  for target in [pid,native]:
   assert os.readlink('/proc/'+str(target)+'/ns/net')==os.readlink('/proc/self/ns/net')
   cap=next(l.split(':',1)[1].strip() for l in Path('/proc/'+str(target)+'/status').read_text().splitlines() if l.startswith('CapEff:'));assert int(cap,16)==0
  host,port=ready['proxy'].rsplit(':',1);assert host=='127.0.0.1'
  def probe(password):
   sock=None
   try:
    sock=socket.create_connection((host,int(port)),timeout=2);sock.settimeout(2);auth=base64.b64encode(('counter-recovery-user:'+password).encode()).decode();destination='http://127.0.0.1:'+str(server.server_port)+'/'
    sock.sendall(('GET '+destination+' HTTP/1.1\r\nHost: 127.0.0.1\r\nProxy-Authorization: Basic '+auth+'\r\nConnection: close\r\n\r\n').encode());reply=http.client.HTTPResponse(sock);reply.begin();return reply.status==200 and reply.read()==payload
   except (OSError,http.client.HTTPException):return False
   finally:
    if sock is not None:sock.close()
  before=hits[0];invalid=probe('negative-control-invalid');assert not invalid and hits[0]==before
  valid=probe('synthetic-counter-recovery-password');assert valid and hits[0]==before+1
  if x.pause_native:os.kill(native,signal.SIGSTOP);paused.add(native)
  os.kill(pid,signal.SIGSTOP);paused.add(pid)
  time.sleep(max(0,(ready['deadline_unix_nano']-time.time_ns())/1e9+.7));over=(time.time_ns()-ready['deadline_unix_nano'])/1e6
  after=probe('synthetic-counter-recovery-password')
  direct=http.client.HTTPConnection('127.0.0.1',server.server_port,timeout=2);direct.request('GET','/');reply=direct.getresponse();assert reply.status==200 and reply.read()==payload;direct.close()
  result={'engine_sha256':x.core_sha,'test_sha256':hashlib.file_digest(open(test,'rb'),'sha256').hexdigest(),'lease_seconds':x.seconds,'ordinary_pending':x.pending,'pause_native_too':x.pause_native,'namespace_isolated':True,'production_signaled':False,'negative_control_rejected':not invalid,'valid_before_pause':valid,'deadline_exceeded_ms':over,'traffic_delivered_after_expiry_while_parent_paused':after,'measurement_used_ptrace':False,'external_origin_verified_alive':True}
  (output/'traffic-observation.json').write_text(json.dumps(result,indent=2))
  os.kill(pid,signal.SIGCONT);paused.remove(pid)
  if x.pause_native:
   time.sleep(.05);os.kill(native,signal.SIGCONT);paused.remove(native)
   result['traffic_delivered_after_both_resume_with_expired_lease']=probe('synthetic-counter-recovery-password')
  rc=process.wait(timeout=35);assert rc==0,'native fixture/replay failed'
  worker=json.loads((control/'worker-result.json').read_text());result['worker_after_resume']=worker
  result['accepted']=not after and not result.get('traffic_delivered_after_both_resume_with_expired_lease',False) and worker.get('final_capture_ok') and worker.get('genuine_positive_final_replay_after_restart') and worker.get('same_known_usage_retained') and not worker['native_alive_after_settlement']
  if x.pending:result['accepted']=result['accepted'] and worker.get('exact_pending_ordinary_receipt_preserved') and worker.get('genuine_final_settled')
  (output/'result.json').write_text(json.dumps(result,indent=2));assert result['accepted'],'lease or genuine final invariant failed';print(json.dumps(result))
 finally:
  for target in list(paused):
   try:os.kill(target,signal.SIGCONT)
   except ProcessLookupError:pass
  if process.poll() is None:os.killpg(process.pid,signal.SIGKILL)
  process.wait(timeout=5);server.shutdown();log.close()
  for name in ['ready.json','worker-result.json']:
   if (control/name).exists():shutil.copyfile(control/name,output/name)
  shutil.rmtree(task)
  owner=int(os.environ.get('SUDO_UID','0'));group=int(os.environ.get('SUDO_GID','0'))
  for p in [output,*output.rglob('*')]:os.chown(p,owner,group)
if __name__=='__main__':main()
