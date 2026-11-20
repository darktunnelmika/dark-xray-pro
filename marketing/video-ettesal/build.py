import subprocess,os,shutil,sys
from playwright.sync_api import sync_playwright
out=sys.argv[1]
with sync_playwright() as p:
    b=p.chromium.launch(); pg=b.new_page(viewport={'width':1080,'height':1920})
    pg.goto('file://'+os.getcwd()+'/v.html'); pg.wait_for_timeout(500)
    starts=pg.evaluate('STARTS'); total=pg.evaluate('TOTAL'); S=starts+[total]; parts=[]
    for i in range(len(S)-1):
        d=f'f{i}'; shutil.rmtree(d,ignore_errors=True); os.makedirs(d)
        n=int(round((S[i+1]-S[i])*30))
        for k in range(n):
            pg.evaluate(f'render({S[i]+k/30})'); pg.screenshot(path=f'{d}/{k:04d}.jpg',type='jpeg',quality=92)
        subprocess.run(['ffmpeg','-nostdin','-v','error','-y','-framerate','30','-i',f'{d}/%04d.jpg','-c:v','libx264','-crf','19','-preset','veryfast','-pix_fmt','yuv420p',f's{i}.tmp.mp4'],check=True)
        os.rename(f's{i}.tmp.mp4',f's{i}.mp4'); parts.append(f"file 's{i}.mp4'"); shutil.rmtree(d)
    b.close()
open('list.txt','w').write('\n'.join(parts))
subprocess.run(['ffmpeg','-nostdin','-v','error','-y','-f','concat','-safe','0','-i','list.txt','-c','copy','silent.mp4'],check=True)
D=total
def chord(fr,vol): return '+'.join(f'{vol}*sin(2*PI*{f}*t)*(0.75+0.25*sin(2*PI*{0.07+i*0.013}*t+{i}))' for i,f in enumerate(fr))
A=chord([110,164.81,220,261.63,329.63,493.88],0.07); F=chord([87.31,174.61,220,261.63,329.63,440],0.07)
bell='+'.join(f"0.16*sin(2*PI*880*(t-{s}))*exp(-5*(t-{s}))*gt(t,{s})+0.08*sin(2*PI*1318.5*(t-{s}))*exp(-6*(t-{s}))*gt(t,{s})" for s in starts)
expr=f"(({A})*(0.5+0.5*cos(2*PI*t/16))+({F})*(0.5-0.5*cos(2*PI*t/16)))+{bell}"
subprocess.run(['ffmpeg','-nostdin','-v','error','-y','-f','lavfi','-i',f"aevalsrc='{expr}':s=44100:d={D}",'-af',f'aecho=0.8:0.6:120|240:0.35|0.2,lowpass=f=5000,afade=t=in:d=1.5,afade=t=out:st={D-2.5}:d=2.5,loudnorm=I=-19:TP=-2,aresample=44100','-c:a','pcm_s16le','music.wav'],check=True)
inp=['-i','music.wav']; f=[]
for i,s in enumerate(starts):
    inp+=['-i',f'e{i}.wav']; f.append(f'[{i+1}:a]adelay={int((s+0.5)*1000)}:all=1[v{i}]')
n=len(starts)
f.append(''.join(f'[v{i}]' for i in range(n))+f'amix=inputs={n}:normalize=0,loudnorm=I=-16:TP=-1.5,aresample=44100[voice]')
f.append('[0:a]volume=0.28[m]'); f.append('[m][voice]amix=inputs=2:normalize=0:duration=first,alimiter=limit=0.9,pan=stereo|c0=c0|c1=c0[out]')
subprocess.run(['ffmpeg','-nostdin','-v','error','-y']+inp+['-filter_complex',';'.join(f),'-map','[out]','-c:a','aac','-b:a','192k','-t',str(D),'mix.m4a'],check=True)
subprocess.run(['ffmpeg','-nostdin','-v','error','-y','-i','silent.mp4','-i','mix.m4a','-map','0:v','-map','1:a','-c:v','copy','-c:a','copy','o.tmp.mp4'],check=True)
os.rename('o.tmp.mp4',out)
