'use strict';
window.createViewStates = ({content:C,state,icon,escape:e,button,header,navigate,renderPage,modal,dismissModal}) => {
  const V=C.viewStates;const model={pages:{},timer:null};
  function set(kind){
    if(kind.startsWith('playback-')){if(!state.player||C.liveRooms.some(v=>v.id===state.player))window.MikuPrototype.startPlayer('orbit');window.MikuPrototype.playerFeatures.showPlaybackState(kind.slice(9));return;}
    if(kind.startsWith('live-')){if(!state.player||!C.liveRooms.some(v=>v.id===state.player))window.MikuPrototype.startPlayer(C.liveRooms[0].id);window.MikuPrototype.playerFeatures.showLiveState(kind.slice(5));return;}
    if(kind==='branch'){if(state.player!=='interactive-orbit')window.MikuPrototype.startPlayer('interactive-orbit');window.MikuPrototype.playerFeatures.showBranch();return;}
    if(kind==='unavailable'){modal(V.unavailable,`<p>${e(V.unavailableBody)}</p>`,button(V.related,'view-related','primary'));return;}
    model.pages[state.page]=kind;renderPage();
  }
  function render(){
    const kind=model.pages[state.page]||'normal';if(kind==='normal')return '';
    const names={account:C.sidebarPages.account.title,settings:C.sidebarPages.settings.title,messages:C.community.title,library:C.nav.library,search:C.nav.search};const name=names[state.page]||C.nav[state.page]||V.content;
    const heading=`${header()}<div class="collection-heading"><div><h1>${e(name)}</h1><p>${e(V.context)}</p></div></div>`;
    if(kind==='loading')return `${heading}<div class="state-loading-grid" role="status" aria-label="${e(V.loading)}">${Array.from({length:8},()=>'<div><i></i><span></span><small></small></div>').join('')}</div>`;
    const offline=kind==='offline';
    return `${heading}<section class="collection-empty page-state-card"><span>${icon(offline?'wifi':'library')}</span><h2>${e(offline?V.offline:V.empty)}</h2><p>${e(offline?V.offlineBody:V.emptyBody)}</p>${button(offline?V.retry:V.explore,offline?'view-retry':'view-explore','secondary',offline?'refresh':'')}</section>`;
  }
  function handle(action){
    if(action==='view-retry'){const page=state.page;model.pages[page]='loading';renderPage();clearTimeout(model.timer);model.timer=setTimeout(()=>{model.pages[page]='normal';if(state.page===page){renderPage();document.querySelector('#main .focusable')?.focus({preventScroll:true});}},650);return true;}
    if(action==='view-explore'){model.pages[state.page]='normal';navigate('home');return true;}
    if(action==='view-related'){dismissModal();navigate('home');return true;}
    return false;
  }
  return {set,render,handle,model};
};
