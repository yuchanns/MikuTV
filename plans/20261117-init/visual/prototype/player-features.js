'use strict';
window.createPlayerFeatures = ({content:C,state,find,icon,escape:e,button,modal,dismissModal,textInput,startPlayer,updateTime,resetHide,filterDanmaku,toast}) => {
  const P=C.playerFeatures;
  const model={quality:P.liveQualities[0],line:0,compose:null,dmMode:'scroll',resolved:false,liveState:'normal',playbackState:'normal'};
  const $=selector=>document.querySelector(selector);
  const options=(labels,current,verb)=>`<div class="option-list">${labels.map((label,i)=>`<button class="focusable option ${i===current?'selected':''}" data-action="${verb}:${i}"><span>${e(label)}</span>${i===current?icon('check'):''}</button>`).join('')}</div>`;
  function episodeData(v){return C.episodeFixtures[v.id]||v.parts||null;}
  function episodes(v){
    if(v.chapters)return `<div class="episode-list-heading"><span>${e(P.chapters)}</span></div><div class="episode-grid">${v.chapters.map((item,i)=>button(`${String(i+1).padStart(2,'0')}  ${item.title}`,`player-chapter:${v.id}:${item.at}`,'episode')).join('')}</div>`;
    const names=episodeData(v);if(!names)return '';
    const available=v.episodeCount||names.length;
    return `<div class="episode-list-heading"><span>${e(v.parts?P.parts:C.courses.includes(v)?P.lessons:P.episodes)}</span><small>${available} / ${names.length}</small></div><div class="episode-grid">${names.map((name,i)=>`<button class="episode focusable ${state.currentEpisode?.id===v.id&&state.currentEpisode.index===i?'selected':''}" data-action="player-episode:${v.id}:${i}" ${i>=available?'disabled':''}><b>${String(i+1).padStart(2,'0')}</b><span>${e(typeof name==='string'?name:name.title)}${i>=available?`<small>${e(P.unreleased)}</small>`:''}</span>${i<available?icon('play'):icon('lock')}</button>`).join('')}</div>`;
  }
  function duration(v){const item=episodeData(v)?.[state.currentEpisode?.id===v.id?state.currentEpisode.index:0];return typeof item==='object'&&item.duration?item.duration:v.duration;}
  function chapterTime(seconds) {
    const minutes=Math.floor(seconds/60);
    return `${String(minutes).padStart(2,'0')}:${String(Math.floor(seconds%60)).padStart(2,'0')}`;
  }
  function focusChapter(index) {
    const cards=[...document.querySelectorAll('.chapter-card')];
    const card=cards[Math.max(0,Math.min(cards.length-1,index))];
    if(!card)return;
    card.focus({preventScroll:true});
    card.scrollIntoView({block:'nearest',inline:'nearest',behavior:state.reduced?'instant':'smooth'});
  }
  function syncChapters() {
    const player=$('.player');
    const v=state.player&&find(state.player);
    const active=state.panel==='chapter-picker';
    const visible=state.overlay==='player'&&v?.chapters?.length&&!v.live&&(active||(!state.panel&&document.activeElement?.matches('.timeline')));
    if(!visible){$('.chapter-picker')?.remove();player?.classList.remove('has-chapter-preview');return;}
    const current=Math.max(0,v.chapters.findLastIndex(chapter=>chapter.at<=state.position));
    let picker=$('.chapter-picker');
    if(!picker){
      player.insertAdjacentHTML('beforeend',`<section class="chapter-picker" role="region" aria-label="${e(P.chapters)}" inert><header><h3>${e(P.chapters)}<span class="chapter-entry-cue" aria-hidden="true">${icon('arrow')}</span></h3><span><b id="chapter-picker-index">${String(current+1).padStart(2,'0')}</b> / ${String(v.chapters.length).padStart(2,'0')}</span></header><div class="chapter-strip">${v.chapters.map((chapter,i)=>`<button class="chapter-card focusable" data-action="player-chapter:${v.id}:${chapter.at}" data-chapter-index="${i}" aria-current="${i===current?'true':'false'}" aria-label="${e(chapterTime(chapter.at)+' '+chapter.title)}"><div class="chapter-image"><img src="assets/${e(chapter.image||v.image)}" alt=""><span>${chapterTime(chapter.at)}</span></div><p>${e(chapter.title)}</p></button>`).join('')}</div></section>`);
      picker=$('.chapter-picker');
      picker.addEventListener('focusin',event=>{
        const card=event.target.closest('.chapter-card');
        if(card)$('#chapter-picker-index').textContent=String(Number(card.dataset.chapterIndex)+1).padStart(2,'0');
      });
    }
    player.classList.add('has-chapter-preview');
    picker.inert=!active;
    const cards=[...picker.querySelectorAll('.chapter-card')];
    cards.forEach((card,index)=>card.setAttribute('aria-current',String(index===current)));
    if(active){delete picker.dataset.previewChapter;return;}
    $('#chapter-picker-index').textContent=String(current+1).padStart(2,'0');
    if(picker.dataset.previewChapter!==String(current)){
      picker.dataset.previewChapter=String(current);
      cards[current]?.scrollIntoView({block:'nearest',inline:'nearest',behavior:state.reduced?'instant':'smooth'});
    }
  }
  function openChapters() {
    const v=state.player&&find(state.player);
    if(!v?.chapters?.length||v.live)return false;
    $('.playback-popover')?.remove();
    const player=$('.player');
    player.classList.remove('has-panel','hidden-controls');
    player.classList.add('has-chapter-picker');
    state.panel='chapter-picker';resetHide();
    const current=Math.max(0,v.chapters.findLastIndex(chapter=>chapter.at<=state.position));
    syncChapters();
    requestAnimationFrame(()=>{if(state.panel==='chapter-picker')focusChapter(current);});
    return true;
  }
  function handleKey(event) {
    if(state.panel!=='chapter-picker'||state.overlay!=='player'||!$('.chapter-picker'))return false;
    const cards=[...document.querySelectorAll('.chapter-card')];
    const index=Math.max(0,cards.indexOf(document.activeElement));
    if(event.key==='Escape'||event.key==='ArrowDown')closePanel();
    else if(event.key==='ArrowLeft'||event.key==='ArrowRight')focusChapter(index+(event.key==='ArrowRight'?1:-1));
    else if(event.key==='Tab')focusChapter((index+(event.shiftKey?-1:1)+cards.length)%cards.length);
    else if(event.key==='Enter'||event.key===' '){const card=cards[index];if(card)handle(card.dataset.action,card);}
    else if(event.key!=='ArrowUp')return false;
    event.preventDefault();
    return true;
  }
  function decorate(v){
    model.playbackState='normal';
    if(v.live){
      model.liveState='normal';
      const quality=$('[data-action="panel:quality"]');quality.textContent=model.quality;
      $('.player-author small').textContent=P.roomInfo;
      quality.insertAdjacentHTML('afterend',button(P.line, 'player-live-lines','control-button text-control'));
      const favorite=$('.player [data-action="favorite"]');favorite.dataset.action='player-live-follow';favorite.setAttribute('aria-label',C.ui.follow);favorite.innerHTML=icon(state.following.has(v.creator)?'check':'plus');
      $('.danmaku').innerHTML=P.chat.filter(item=>filterDanmaku([item.text]).length).slice(0,5).map(item=>`<span>${e(item.text)}</span>`).join('');
    }
    const names=episodeData(v);
    if(names){
      const i=state.currentEpisode?.id===v.id?state.currentEpisode.index:0;
      const item=names[i];const label=typeof item==='string'?item:item.title;
      $('.player-top h2').textContent=`${v.short||v.title} · ${String(i+1).padStart(2,'0')} ${label}`;
      const available=v.episodeCount||names.length;
      $('.player [data-action="previous"]').disabled=i===0;$('.player [data-action="next"]').disabled=i>=available-1;
    }
    if(v.interactive)model.resolved=false;
    const total=duration(v).split(':').map(Number).reduce((sum,n)=>sum*60+n,0);
    $('.timeline').insertAdjacentHTML('beforeend',(v.chapters||[]).slice(1).filter(chapter=>chapter.at>0&&chapter.at<total).map(chapter=>`<i class="chapter-marker" aria-hidden="true" style="left:${chapter.at/total*100}%"></i>`).join(''));
  }
  function liveMenu(type){
    $('.live-state-card')?.remove();$('.playback-popover')?.remove();$('.player').classList.remove('has-panel');state.panel=type;resetHide();
    const labels=type==='live-line'?P.lines:P.liveQualities;
    const current=type==='live-line'?model.line:P.liveQualities.indexOf(model.quality);
    $('.player').insertAdjacentHTML('beforeend',`<section class="playback-popover menu-live" role="dialog"><header><h3>${e(type==='live-line'?P.line:P.liveQuality)}</h3></header><div class="playback-menu-body">${options(labels,current,type==='live-line'?'player-line':'player-live-quality')}</div></section>`);
    requestAnimationFrame(()=>$('.playback-popover .selected')?.focus({preventScroll:true}));
  }
  function liveChat(){
    $('.playback-popover')?.remove();state.panel='comments';$('.player').classList.add('has-panel');$('.player-panel').classList.add('comments-panel');resetHide();
    $('.player-panel').innerHTML=`<section class="live-chat"><header><span class="live-badge"><i></i>LIVE</span><h3>${e(P.chatTitle)}</h3><small>${e(find(state.player).views)} ${e(P.viewers)}</small></header><div class="live-chat-scroll scroll-region focusable" tabindex="0">${P.chat.map(item=>`<div class="live-chat-message ${item.own?'own':''}"><span>${e(item.name)}</span><p>${e(item.text)}</p></div>`).join('')}</div>${button(model.liveState==='muted'?P.muted:P.sendChat,'player-compose:live','live-chat-compose','comment',model.liveState==='muted'?'disabled':'')}</section>`;
    requestAnimationFrame(()=>$('.live-chat-scroll')?.focus({preventScroll:true}));
  }
  function roomInfo(){
    const v=find(state.player);
    modal(v.short||v.title,`<div class="live-room-info"><img src="assets/${v.image}" alt=""><div><strong>${e(v.creator)}</strong><span>${e(v.category)}</span><p>${e(P.roomDescription)}</p><small>${e(P.roomNumber)} 392731</small></div></div>`,button(state.following.has(v.creator)?C.ui.followed:C.ui.follow,'player-live-follow','primary',state.following.has(v.creator)?'check':'plus'));
  }
  function compose(kind){
    model.compose=kind;model.dmMode='scroll';
    const title=kind==='live'?P.sendChat:P.sendDanmaku;
    modal(title,`<input id="player-message-draft" type="hidden" maxlength="${kind==='live'?30:80}" value=""><p class="player-message-preview">${e(P.placeholder)}</p>${kind==='danmaku'?`<div class="dm-compose-modes">${['scroll','top','bottom'].map(key=>button(P.dmModes[key],`player-dm-mode:${key}`,`chip ${key==='scroll'?'selected':''}`)).join('')}</div>`:''}<p id="player-message-error" class="form-error"></p>`,button(P.edit,'player-edit-message','secondary')+button(P.send,'player-send-message','primary','send','disabled'));
    requestAnimationFrame(editMessage);
  }
  function editMessage(){
    const field=$('#player-message-draft');textInput.openField(field,{title:model.compose==='live'?P.sendChat:P.sendDanmaku,returnFocus:$('[data-action="player-edit-message"]'),onCommit(text){$('.player-message-preview').textContent=text||P.placeholder;const send=$('[data-action="player-send-message"]');send.disabled=!text.trim();requestAnimationFrame(()=>send.disabled?$('[data-action="player-edit-message"]').focus():send.focus());}});
  }
  function showBranch(){
    if(!state.player||!find(state.player).interactive||$('.branch-choice'))return;
    state.playing=false;state.panel='branch';$('.player').classList.add('hidden-controls');
    $('.player').insertAdjacentHTML('beforeend',`<section class="branch-choice"><span>${e(P.interactive)}</span><h2>${e(P.branchQuestion)}</h2><div>${P.branches.map((branch,i)=>`<button class="focusable branch-option" data-action="player-branch:${i}"><b>0${i+1}</b><span><strong>${e(branch.title)}</strong><small>${e(branch.note)}</small></span></button>`).join('')}</div></section>`);
    requestAnimationFrame(()=>$('.branch-option')?.focus({preventScroll:true}));
  }
  function showLiveState(kind){
    model.liveState=kind;$('.live-state-card')?.remove();$('.playback-popover')?.remove();$('.player')?.classList.remove('has-panel');
    if(kind==='normal'){state.playing=true;state.panel=null;return;}
    if(kind==='muted'){liveChat();return;}
    state.playing=false;state.panel='live-status';$('.player').classList.add('hidden-controls');
    $('.player').insertAdjacentHTML('beforeend',`<section class="live-state-card">${icon(kind==='ended'?'live':'wifi')}<h2>${e(kind==='ended'?P.ended:P.disconnected)}</h2><p>${e(kind==='ended'?P.endedBody:P.disconnectedBody)}</p>${button(kind==='ended'?P.moreLive:P.retry,kind==='ended'?'player-more-live':'player-live-retry','primary',kind==='ended'?'':'refresh')}${kind==='disconnected'?button(P.line,'player-live-lines','secondary','refresh'):''}</section>`);
    requestAnimationFrame(()=>$('.live-state-card button')?.focus({preventScroll:true}));
  }
  function showPlaybackState(kind){
    model.playbackState=kind;
    $('.playback-state-card')?.remove();$('.playback-popover')?.remove();$('.player').classList.remove('has-panel');
    state.playing=false;state.panel='playback-status';$('.player').classList.add('hidden-controls');
    const loading=kind==='buffering';const ended=kind==='ended';
    $('.player').insertAdjacentHTML('beforeend',`<section class="playback-state-card" role="status" tabindex="-1">${loading?'<i class="playback-spinner"></i>':icon(ended?'check':'wifi')}<h2>${e(loading?P.buffering:ended?P.playbackEnded:P.playbackFailed)}</h2>${loading?'':`<p>${e(ended?P.endedNote:P.failedNote)}</p>${button(ended?P.replay:P.retry,'player-playback-retry','primary',ended?'play':'refresh')}`}</section>`);
    requestAnimationFrame(()=>($('.playback-state-card button')||$('.playback-state-card')).focus({preventScroll:true}));
  }
  function closePanel(){
    if(state.panel==='chapter-picker'){state.panel=null;$('.player').classList.remove('has-chapter-picker');$('[data-action="seek"]')?.focus({preventScroll:true});syncChapters();resetHide();return true;}
    if(state.panel==='playback-status'){state.panel=null;$('.playback-state-card')?.remove();$('.player').classList.remove('hidden-controls');$('[data-action="pause"]')?.focus({preventScroll:true});return true;}
    if(state.panel==='live-line'){state.panel=null;$('.playback-popover')?.remove();$('[data-action="player-live-lines"]')?.focus({preventScroll:true});resetHide();return true;}
    if(state.panel==='branch'){state.panel=null;$('.branch-choice')?.remove();$('.player').classList.remove('hidden-controls');$('[data-action="pause"]')?.focus({preventScroll:true});return true;}
    if(state.panel==='live-status'){state.panel=null;$('.live-state-card')?.remove();$('.player').classList.remove('hidden-controls');$('[data-action="pause"]')?.focus({preventScroll:true});return true;}
    return false;
  }
  function handle(action,element){
    const [verb,key,index]=action.split(':');const live=!!(state.player&&find(state.player).live);
    if(verb==='player-chapter'){
      if(state.player===key){
        if(state.scrub){state.playing=state.scrub.wasPlaying;state.scrub=null;$('.player').classList.remove('seeking');}
        state.position=Number(index);window.MikuPrototype.closePanel();updateTime();
      }else startPlayer(key,{resumePosition:Number(index)});
      return true;
    }
    if(verb==='player-episode'){state.currentEpisode={id:key,index:Number(index)};startPlayer(key,{resumePosition:0});return true;}
    if(verb==='panel'&&live&&['quality','comments','description'].includes(key)){if(key==='quality')liveMenu('quality');if(key==='comments')liveChat();if(key==='description')roomInfo();return true;}
    if(verb==='player-live-lines'){liveMenu('live-line');return true;}
    if(verb==='player-live-quality'){model.quality=P.liveQualities[Number(key)];$('.playback-popover').remove();state.panel=null;$('[data-action="panel:quality"]').textContent=model.quality;$('[data-action="panel:quality"]').focus();resetHide();return true;}
    if(verb==='player-line'){model.line=Number(key);closePanel();if(model.liveState==='disconnected'){model.liveState='normal';state.playing=true;}toast(P.lineChanged);return true;}
    if(verb==='player-live-follow'){const v=find(state.player);state.following.has(v.creator)?state.following.delete(v.creator):state.following.add(v.creator);if(state.overlay==='modal'){dismissModal();}else element.innerHTML=icon(state.following.has(v.creator)?'check':'plus');toast(state.following.has(v.creator)?C.ui.followed:C.ui.follow);return true;}
    if(verb==='player-compose'){compose(key);return true;}
    if(verb==='player-edit-message'){editMessage();return true;}
    if(verb==='player-dm-mode'){model.dmMode=key;document.querySelectorAll('.dm-compose-modes .chip').forEach(item=>item.classList.toggle('selected',item===element));return true;}
    if(verb==='player-send-message'){
      const text=$('#player-message-draft').value.trim();if(!text)return true;
      if(model.liveState==='muted'&&model.compose==='live'){$('#player-message-error').textContent=P.muted;return true;}
      if(model.compose==='live')P.chat.push({name:C.discussion.me,text,own:true});
      else {const area=$('.danmaku');area.insertAdjacentHTML('beforeend',`<span class="own-danmaku dm-${model.dmMode}">${e(text)}</span>`);const own=area.lastElementChild;setTimeout(()=>own.remove(),11000);}
      dismissModal();if(model.compose==='live')liveChat();toast(P.sent);return true;
    }
    if(verb==='player-branch'){model.resolved=true;$('.branch-choice').remove();state.panel=null;state.position=Number(key)===0?90:180;state.playing=true;$('.player').classList.remove('hidden-controls');$('.player-top h2').textContent=P.branches[Number(key)].title;updateTime();resetHide();return true;}
    if(verb==='player-playback-retry'){const ended=model.playbackState==='ended';if(ended)state.position=0;closePanel();state.playing=true;$('.player').classList.remove('paused');$('[data-action="pause"]').innerHTML=icon('pause');updateTime();resetHide();return true;}
    if(verb==='player-live-retry'){showLiveState('normal');state.panel=null;$('.player').classList.remove('hidden-controls');$('.live-state-card')?.remove();resetHide();return true;}
    if(verb==='player-more-live'){window.MikuPrototype.navigate('live');return true;}
    return false;
  }
  return {decorate,episodes,duration,handle,handleKey,openChapters,syncChapters,closePanel,showBranch,showLiveState,showPlaybackState,model,tick(){if(!state.player||state.scrub||state.overlay==='modal'||!state.playing)return;const v=find(state.player);if(v.interactive&&!model.resolved&&state.position>=42){showBranch();return;}const total=duration(v).split(':').map(Number).reduce((sum,n)=>sum*60+n,0);if(!v.live&&!state.trial&&state.position>=total)showPlaybackState('ended');},advance(delta){if(!state.player)return false;const v=find(state.player);const names=episodeData(v);if(!names)return false;const index=(state.currentEpisode?.id===v.id?state.currentEpisode.index:0)+delta;if(index>=0&&index<(v.episodeCount||names.length)){state.currentEpisode={id:v.id,index};startPlayer(v.id,{resumePosition:0});}return true;}};
};
