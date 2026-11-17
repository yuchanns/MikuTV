/* Entitlement transitions use local sample data, never a real purchase. */
window.createContentAccess = ({content,state,find,icon,escape,button,modal,dismissModal,login,startPlayer,updateTime,toast}) => {
  const A=content.access;
  state.owned=new Set();state.trialsUsed=new Set();state.trial=null;state.trialEnded=false;
  const kind=v=>v.access==='purchase'&&!state.owned.has(v.id)?'purchase':v.access==='vip'&&!state.member?'vip':null;
  const label=v=>kind(v)==='vip'?A.watchVip:kind(v)==='purchase'?A.purchaseBadge:content.ui.play;
  function show(id,ended=false){
    const v=find(id);const type=kind(v);state.pendingContent=id;
    if(!state.player&&state.detail!==id){state.detail=id;state.overlay='detail';window.MikuPrototype.openDetail(id);}
    const canTrial=!ended&&v.trialSeconds>0&&!state.trialsUsed.has(id);
    const body=`<div class="content-gate-heading">${icon('lock')}<span>${escape(v.short||v.title)}</span></div><p>${escape(ended?A.trialEndedBody:type==='purchase'?A.purchaseBody:A.vipBody)}</p><div class="content-phone" hidden><p>${escape(type==='purchase'?A.purchasePhoneBody:content.rights.phoneBody)}</p><span class="official-link">${escape(A.official)}</span></div>`;
    const actions=[canTrial?button(A.trial,'content-trial','primary','play'):'',!state.account?button(A.login,'content-login','secondary'):'',button(type==='purchase'?A.purchase:A.phone,'content-phone',canTrial?'secondary':'primary'),button(type==='purchase'?A.refreshPurchase:A.refresh,'content-refresh','secondary'),button(A.return,'content-return','quiet-action')].join('');
    modal(ended?A.trialEndedTitle:type==='purchase'?A.purchaseTitle:A.vipTitle,body,actions);
    document.querySelector('.state-panel .modal')?.classList.add('content-access-modal');
  }
  function authorize(id,options={}){
    const v=find(id);const locked=kind(v);
    const trial=locked&&options.trial&&v.trialSeconds>0&&!state.trialsUsed.has(id);
    if(locked&&!trial){show(id);return null;}
    state.trial=trial?{id,limit:v.trialSeconds}:null;state.trialEnded=false;
    if(trial)state.trialsUsed.add(id);
    return {position:options.resumePosition??(trial?0:542)};
  }
  function banner(){return state.trial?`<div class="trial-banner">${icon('lock')}<span>${escape(A.trialState)}</span><i></i><span>${escape(A.trialLeft)} <b id="trial-remaining">03:00</b></span></div>`:'';}
  function tick(){
    if(!state.trial)return;
    const remaining=Math.max(0,state.trial.limit-state.position);
    const label=document.querySelector('#trial-remaining');
    if(label)label.textContent=`${String(Math.floor(remaining/60)).padStart(2,'0')}:${String(Math.floor(remaining%60)).padStart(2,'0')}`;
    if(remaining===0&&!state.trialEnded){
      state.trialEnded=true;state.playing=false;
      document.querySelector('.player')?.classList.add('paused');
      const pause=document.querySelector('[data-action="pause"]');if(pause)pause.innerHTML=icon('play');
      if(state.overlay!=='modal')show(state.trial.id,true);
    }
  }
  function promote(){
    if(state.trial&&!kind(find(state.trial.id))){
      state.trial=null;state.trialEnded=false;state.playing=true;
      document.querySelector('.trial-banner')?.remove();document.querySelector('.player')?.classList.remove('paused');
      const pause=document.querySelector('[data-action="pause"]');if(pause)pause.innerHTML=icon('pause');
    }
  }
  function handle(action,element){
    if(!action.startsWith('content-'))return false;
    const id=state.pendingContent;const v=find(id);
    if(action==='content-trial'){dismissModal();startPlayer(id,{trial:true,resumePosition:0});}
    if(action==='content-phone'){document.querySelector('.content-phone').hidden=false;}
    if(action==='content-login'){state.resumeContentAfterLogin=id;dismissModal();login();}
    if(action==='content-return'){
      dismissModal();state.trial=null;state.trialEnded=false;state.player=null;state.panel=null;window.MikuPrototype.openDetail(id);
    }
    if(action==='content-refresh'){
      element.disabled=true;element.textContent=content.rights.refreshing;
      const position=state.player===id?state.position:0;
      setTimeout(()=>{
        if(v.access==='purchase')state.owned.add(id);else state.member=true;
        state.account=true;state.scenario=state.member?'member':'free';document.querySelector('#scenario-select').value=state.scenario;
        dismissModal();promote();startPlayer(id,{resumePosition:position});toast(A.entitled);
      },650);
    }
    return true;
  }
  function previewEnd(){
    state.member=false;state.account=true;state.scenario='free';state.trialsUsed.delete('show-4');
    startPlayer('show-4',{trial:true,resumePosition:180});updateTime();
  }
  return {kind,label,show,authorize,banner,tick,promote,handle,previewEnd};
};
