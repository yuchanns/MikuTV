'use strict';
window.createAuthFlow = ({content:C,state,icon,escape:e,button,modal,dismissModal,complete}) => {
  const A=C.auth;
  const model={phase:'waiting',deadline:0,timer:null,finishTimer:null,session:0};
  function remaining(){const seconds=Math.max(0,Math.ceil((model.deadline-Date.now())/1000));return `${String(Math.floor(seconds/60)).padStart(2,'0')}:${String(seconds%60).padStart(2,'0')}`;}
  function render(){
    if(document.querySelector('.auth-modal'))dismissModal();
    const phase=model.phase;
    const content=phase==='waiting'?`<div class="auth-qr"><img src="assets/auth-qr.svg" alt="${e(A.qr)}"></div><div class="auth-status"><i class="auth-pulse"></i><span>${e(A.waiting)}</span><time id="auth-time">${remaining()}</time></div>`:phase==='scanned'?`<div class="auth-confirm">${icon('phone')}<span class="auth-check">${icon('check')}</span></div><h3>${e(A.scanned)}</h3><p class="auth-phase-note">${e(A.phoneConfirm)}</p>`:phase==='success'?`<div class="auth-confirm success">${icon('check')}</div><h3>${e(A.success)}</h3><p class="auth-phase-note">${e(A.continuing)}</p>`:`<div class="auth-qr expired"><img src="assets/auth-qr.svg" alt=""><div>${icon('refresh')}<strong>${e(A.expired)}</strong></div></div>`;
    modal(A.title,`<p class="auth-description">${e(phase==='waiting'?A.body:phase==='expired'?A.expiredBody:A.secure)}</p><div class="auth-stage">${content}</div>`,phase==='expired'?button(A.refresh,'auth-refresh','primary','refresh'):'');
    const panel=document.querySelector('.state-panel .modal');panel.classList.add('auth-modal');
    requestAnimationFrame(()=>{const target=panel.querySelector('button')||panel;target.focus({preventScroll:true});});
  }
  function start(){
    clearInterval(model.timer);clearTimeout(model.finishTimer);model.session++;model.phase='waiting';model.deadline=Date.now()+180000;render();
    model.timer=setInterval(()=>{
      if(!document.querySelector('.auth-modal')){clearInterval(model.timer);return;}
      if(model.phase!=='waiting')return;
      const time=document.querySelector('#auth-time');if(time)time.textContent=remaining();
      if(Date.now()>=model.deadline)setPhase('expired');
    },1000);
  }
  function setPhase(phase,session=model.session){
    if(!document.querySelector('.auth-modal')||session!==model.session)return false;
    clearTimeout(model.finishTimer);model.phase=phase;render();
    if(phase==='success')model.finishTimer=setTimeout(()=>{if(!document.querySelector('.auth-modal')||model.phase!=='success'||session!==model.session)return;clearInterval(model.timer);complete();},950);
  }
  return {open:start,setPhase,preview(phase){if(!document.querySelector('.auth-modal'))start();setPhase(phase);},cancel(){clearInterval(model.timer);clearTimeout(model.finishTimer);model.session++;},model,handle(action){if(action==='auth-refresh'){start();return true;}return false;}};
};
