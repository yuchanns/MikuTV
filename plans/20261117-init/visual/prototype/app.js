/* The prototype uses local demonstration data and never contacts a live source. */
(() => {
  'use strict';
  const C = window.CONTENT;
  const U = C.ui;
  const R = C.rights;
  const M = C.playbackMenus;
  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
  const escape = (value) => String(value ?? '').replace(/[&<>"']/g, (char) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
  const asset = (file) => `assets/${file}`;
  const icon = (name) => (window.LUCIDE_ICONS[name] || window.LUCIDE_ICONS.info).replace('<svg', `<svg data-icon="${name}" aria-hidden="true"`);
  const button = (label, action, type = 'secondary', glyph = '', extra = '') => `<button class="focusable ${type}" data-action="${action}" ${extra}>${glyph ? icon(glyph) : ''}${escape(label)}</button>`;
  const allVideos = [...C.heroes, ...C.videos, ...C.shows, ...C.liveRooms, ...C.courses];
  const find = (id) => allVideos.find((item) => item.id === id) || C.heroes[0];
  const state = {
    page:'home', homeTab:'forYou', hero:0, category:U.any, libraryTab:'history',
    query:'', searchKind:'all', sort:0, later:new Set(['mountains','forest']),
    favorites:new Set(['orbit','city']), likes:new Set(), following:new Set([...C.heroes,...C.videos].map(v=>v.creator)),
    tracked:new Set(['show-1']), history:['orbit','mountains','city','forest'],
    overlay:null, detail:null, detailTab:'about', detailPart:0, detailHistory:[], player:null, playing:true,
    position:542, quality:'1080P', subtitles:U.off, danmaku:true, reduced:false,
    preview:true, focus:null, returnFocus:null, autoHide:null, toastTimer:null,
    account:true, member:false, scenario:'free', coins:1, historyStack:[], tour:false, panel:null
  };
  const discussion = window.createDiscussion({
    content:C, icon, escape, button, refresh:refreshDiscussion, openModal:modal, dismissModal, toast:showToast,
    editText(field,options){textInput.openField(field,options);},
    seek(seconds){if(!state.player)startPlayer(state.detail);state.position=seconds;updateTime();}
  });
  function refreshDiscussion({action,position,mode}) {
    const target=mode==='panel'?$('.player-panel'):$('#detail-content');if(!target)return;
    target.innerHTML=discussion.render(mode);
    const scroller=$('.discussion-scroll',target);if(position!==null&&scroller)scroller.scrollTop=position;
    if(action)requestAnimationFrame(()=>{focusAction(action);if(position===null)document.activeElement?.scrollIntoView({block:'nearest',behavior:'smooth'});});
  }
  const contentAccess=window.createContentAccess({content:C,state,find,icon,escape,button,modal,dismissModal,login,startPlayer,updateTime,toast:showToast});
  const themePages=window.createThemePages({state,icon,escape,button,modal,dismissModal,packages:()=>pluginPages.model.items,preferenceRow:options=>sidebarPages.preferenceRow(options),browseThemes:()=>pluginPages.openThemes()});
  const sidebarPages=window.createSidebarPages({content:C,state,icon,escape,button,header,modal,dismissModal,navigate,renderPage,login,rightsNotice,toast:showToast,unreadMessages:()=>communityPages.unread(),renderPlugins:()=>pluginPages.renderSettings(),renderThemeRow:()=>themePages.renderRow(),switchAccount:()=>pluginPages.openAccounts(),accountSignedOut:()=>pluginPages.accountSignedOut()});
  const libraryPages=window.createLibraryPages({content:C,state,icon,escape,button,header,card,find,modal,dismissModal,renderPage,startPlayer,login,toast:showToast});
  const explorePages=window.createExplorePages({content:C,state,icon,escape,button,header,card,modal,dismissModal,navigate,renderPage,updateSearch});
  const authFlow=window.createAuthFlow({content:C,state,icon,escape,button,modal,dismissModal,complete:()=>perform('login')});
  const textInput=window.createTextInput({content:C,state,icon,escape,button});
  const communityPages=window.createCommunityPages({content:C,state,icon,escape,button,header,card,find,allVideos,modal,dismissModal,navigate,renderPage,textInput,toast:showToast});
  const playerFeatures=window.createPlayerFeatures({content:C,state,find,icon,escape,button,modal,dismissModal,textInput,startPlayer,updateTime,resetHide,filterDanmaku:sidebarPages.filterDanmaku,toast:showToast});
  const viewStates=window.createViewStates({content:C,state,icon,escape,button,header,navigate,renderPage,modal,dismissModal});
  const pluginPages=window.createPluginPages({content:C,state,settings:sidebarPages,themes:themePages,icon,escape,button,header,modal,dismissModal,navigate,renderPage,toast:showToast,login});
  const meta = (v) => `<div class="meta"><span class="badge">${escape(v.tag || '4K')}</span><span>${escape(v.category)}</span><i class="dot"></i><span>${escape(v.duration)}</span></div>`;
  function resize() {
    const viewport = $('.viewport');
    const scale = Math.min(viewport.clientWidth / 1920, viewport.clientHeight / 1080);
    $('#stage').style.transform = `scale(${scale})`;
    viewport.style.setProperty('--scale',scale);
  }
  function renderRail() {
    const currentNav=({entries:'entries',messages:'account',index:'series',calendar:'series',collection:explorePages.model.parent})[state.page]||state.page;
    const entries=pluginPages.model.currentEntry?['home','search','following','series','live','library']:[];
    const row=(id,label,glyph,extra='')=>`<button class="nav-button focusable ${currentNav===id?'active':''}" data-action="${id==='entries'?'entry-open':id==='settings'||id==='account'?id:'nav:'+id}" aria-label="${escape(label)}" title="${escape(label)}"><span class="nav-symbol">${extra||icon(glyph)}</span><span class="nav-label">${escape(label)}</span></button>`;
    $('#rail').innerHTML=`<div class="logo"><img class="brand-mark" src="assets/app-icon/mark.svg" alt=""><div class="logo-word">${escape(C.brand)}</div></div><nav class="nav-items">${entries.map(id=>row(id,C.nav[id],id)).join('')}</nav><div class="nav-spacer"></div><div class="nav-footer">${row('entries','\u5207\u6362\u5165\u53e3','switch')}${row('settings',C.nav.settings,'settings')}${pluginPages.model.currentEntry?row('account',state.account?(state.accountName||C.nav.account):C.sidebarPages.account.guestLabel,'user',state.account?'<span class="nav-avatar">'+escape(state.accountInitial||'H')+'</span>':''):''}</div>`;
  }
  function header(tabs = [], selected = '', action = 'tab') {
    return `<div class="page-header"><div class="tabs">${tabs.map(([id,label]) => button(label,`${action}:${id}`,`tab ${selected === id ? 'selected' : ''}`)).join('')}</div></div>`;
  }
  function card(v, options = {}) {
    const live=options.live||v.live;
    const info=live?`${escape(v.creator)}<span>·</span>${escape(v.category)}`:options.show?`${escape(v.category)}<span>·</span>${escape(v.views)}${escape(C.sidebarPages.rating)}`:`<i class="up-dot"></i>${escape(v.creator)}<span>·</span><span>${escape(v.views||'32.6')}${U.playCount}</span>`;
    return `<button class="video-card focusable" data-action="${live?'play':'detail'}:${v.id}" aria-label="${escape(v.short||v.title)}"><div class="card-image"><img src="${asset(v.image)}" alt="" loading="lazy">${live?'<span class="live-label"><span class="live-badge"><i></i>LIVE</span></span>':`<span class="card-tag">${escape(v.tag||'4K')}</span>`}<span class="duration">${escape(live?v.views:v.duration)}</span>${options.progress&&v.progress?`<div class="progress"><i style="width:${v.progress}%"></i></div>`:''}</div><div class="card-title">${escape(v.short||v.title)}</div><div class="card-info">${info}</div></button>`;
  }
  function shelf(title, note, videos, options = {}) {
    return `<section class="shelf ${options.className || ''}"><div class="shelf-header"><h2>${options.editorial?`<span data-editorial="series">${escape(window.EDITORIAL_TIME.series())}</span>`:escape(title)}<small>${escape(note)}</small></h2>${button(U.all,explorePages.registerShelf(title,videos,options),'see-all')}</div><div class="cards">${videos.length?videos.map((v) => card(v,options)).join(''):empty(R.emptyCategory,R.emptyCategoryHint)}</div></section>`;
  }
  function hero(v, options = {}) {
    return `<section class="hero"><div class="hero-art ${state.hero !== 0 || options.live ? 'alt' : ''}"><img class="hero-scene" src="${asset(v.image)}" alt=""></div><div class="hero-copy"><div class="eyebrow">${options.live?'LIVE, IN THE MOMENT':options.series?escape(v.eyebrow):`<span data-editorial="feature" data-feature-index="${state.hero}">${escape(window.EDITORIAL_TIME.feature(state.hero))}</span>`}</div><h1>${escape(options.title || v.title)}</h1>${options.live?`<div class="meta"><span class="live-badge"><i></i>${escape(U.liveNow)}</span><span>${escape(v.category)}</span><i class="dot"></i><span>${escape(v.views)} ${escape(C.sidebarPages.liveViewers)}</span></div>`:meta(v)}<p class="hero-description">${escape(options.description || v.description || U.seriesSub).replace(/\n/g,'<br>')}</p><div class="actions">${button(options.live ? C.sidebarPages.liveEnter : U.play,`play:${v.id}`,'primary','play')}${button(U.details,`detail:${v.id}`,'secondary','info')}${options.series?button(state.tracked.has(v.id)?U.seasonFollowed:U.seasonFollow,`track:${v.id}`,'secondary',state.tracked.has(v.id)?'check':'plus'):options.live?'':button('',`later:${v.id}`,'icon-button',state.later.has(v.id)?'check':'plus',`aria-label="${escape(U.later)}" title="${escape(U.later)}"`)}</div></div><span class="hero-caption">${options.live ? 'MUSIC / LIVE / TOGETHER' : 'A BIGGER SCREEN. A BIGGER WORLD.'}</span><div class="hero-pager"><span>0${state.hero + 1} / 03</span>${C.heroes.map((_, i) => `<button class="focusable ${state.hero === i ? 'selected' : ''}" aria-label="Feature ${i+1}" data-action="hero:${i}"></button>`).join('')}</div></section>`;
  }
  function home() {
    const tabs = explorePages.homeTabs();
    if(state.homeTab==='ranking')return explorePages.ranking();
    if(state.homeTab !== 'forYou') {
      const filtered = filterVideos([...C.heroes,...C.videos],state.category);
      return `${header(tabs,state.homeTab)}<div class="page-title"><div class="eyebrow">${state.homeTab === 'trending' ? 'IN THE SPOTLIGHT' : 'FOLLOW YOUR CURIOSITY'}</div><h1>${escape(state.homeTab === 'trending' ? U.trending : U.categories)}</h1><p>${escape(U.picksNote)}</p></div>${chips([U.any,...new Set([...C.heroes,...C.videos].map(v => v.category.split(' · ')[0]))])}<div class="page-grid">${filtered.map(v => card(v)).join('')}</div>`;
    }
    return `${header(tabs,state.homeTab)}${hero(C.heroes[state.hero])}${shelf(U.continue,U.continueNote,[...C.heroes,...C.videos].slice(0,7),{progress:true})}${shelf(U.picks,U.picksNote,[...C.videos,...C.heroes])}${shelf(U.up,U.latest,[...C.videos].reverse())}${homeRecommendations()}`;
  }
  function recommendationColumns() {
    const main=$('#main');
    const styles=getComputedStyle(main);
    const pixels=property=>parseFloat(styles.getPropertyValue(property));
    const stride=pixels('--video-card-width')+pixels('--video-card-gap');
    return Math.max(1,Math.ceil((main.clientWidth-pixels('--shelf-left'))/stride));
  }
  function homeRecommendations() {
    const items=[...C.videos,...C.heroes];
    const offset=state.recommendationOffset||0;
    const ordered=[...items.slice(offset),...items.slice(0,offset)];
    const columns=recommendationColumns();
    const rows=[];
    for(let i=0;i<ordered.length;i+=columns){
      const cards=ordered.slice(i,i+columns).map(item=>card(item).replace(`data-action="detail:${item.id}"`,`data-action="home-recommendation:${item.id}"`));
      rows.push(`<div class="cards home-recommendation-row">${cards.join('')}</div>`);
    }
    return `<section class="shelf home-recommendations" data-columns="${columns}"><div class="shelf-header"><h2>${escape(U.recommendations)}</h2>${button(U.refreshRecommendations,'recommendations-refresh','see-all','refresh')}</div>${rows.join('')}</section>`;
  }
  function chips(labels, action = 'category', selected = state.category) {
    return `<div class="category-chips">${labels.map(label => button(label,`${action}:${label}`,`chip ${selected === label ? 'selected' : ''}`)).join('')}</div>`;
  }
  function filterVideos(videos, category) {
    if(category === U.any) return videos;
    return videos.filter(v => v.category.includes(category));
  }
  function pageTitle(eyebrow,title,description) {
    return `<div class="page-title"><div class="eyebrow">${eyebrow}</div><h1>${escape(title)}</h1><p>${escape(description)}</p></div>`;
  }
  function following() {
    const updates=[...C.heroes,...C.videos].filter(v=>state.following.has(v.creator));
    const creators=[...new Map(updates.map(v=>[v.creator,v])).values()];
    const rooms=C.liveRooms.filter(v=>state.following.has(v.creator));
    const live=state.followingView==='live';
    return `${header([['all',C.sidebarPages.followingAll],['live',C.sidebarPages.followingLive]],state.followingView||'all','following-view')}${pageTitle('FROM YOUR FAVORITE CREATORS',U.followingTitle,U.followingSub)}<div class="avatar-row">${creators.map(v=>`<button class="avatar-button focusable" data-action="creator:${v.id}"><div class="avatar"><img src="${asset(v.image)}" alt=""></div><span>${escape(v.creator)}</span></button>`).join('')}</div>${live?shelf(U.liveFollow,'LIVE NOW',rooms,{live:true}):shelf(U.latest,U.up,updates.slice().reverse())}${live?'':shelf(U.liveFollow,'LIVE NOW',rooms,{live:true})}`;
  }
  function series() {
    const first={...C.shows[0],eyebrow:C.sidebarPages.seriesEyebrow};
    return `${header(C.seriesGenres.map(label=>[label,label]),state.category,'category')}${explorePages.navTools()}${hero(first,{series:true})}${shelf(U.seriesPicks,'CURATED COLLECTION',filterVideos(C.shows,state.category),{className:'series-shelf',show:true,editorial:true})}${shelf(U.newEpisodes,'YOUR NEXT EPISODE',C.shows.filter(v=>state.tracked.has(v.id)),{progress:true,show:true})}`;
  }
  function live() {
    const rooms=filterVideos(C.liveRooms,state.category);
    return `${header([U.any,...C.liveGenres.filter(label=>label!==U.any)].map(label=>[label,label]),state.category,'category')}${hero(C.liveRooms[0],{live:true})}${shelf(C.sidebarPages.allRooms,'LIVE NOW',rooms,{live:true})}${shelf(C.sidebarPages.followRooms,'YOUR CREATORS, LIVE',C.liveRooms.filter(v=>state.following.has(v.creator)),{live:true})}`;
  }
  function library() {
    const tabs = [['history',U.history],['watchlater',U.watchlater],['favorites',U.favorites],['tracking',U.tracking]];
    const ids = state.libraryTab === 'history' ? state.history : [...(state.libraryTab === 'watchlater' ? state.later : state.libraryTab === 'favorites' ? state.favorites : state.tracked)];
    return `${header(tabs,state.libraryTab,'library')}${pageTitle('A COLLECTION OF YOU',U.libraryTitle,U.librarySub)}<div class="library-summary-line"><span>${escape(tabs.find(([id])=>id===state.libraryTab)[1])}</span><small>${ids.length} ${escape(C.sidebarPages.libraryCount)}</small></div><div class="page-grid library-results">${ids.length?ids.map(id=>card(find(id),{progress:state.libraryTab==='history'})).join(''):empty(C.sidebarPages.libraryEmpty,C.sidebarPages.libraryEmptyNote)}</div>`;
  }
  function empty(title,note) { return `<div class="empty">${icon('library')}<div>${escape(title)}</div><small>${escape(note)}</small></div>`; }
  function search() {
    return `${header()}${pageTitle('A LITTLE CURIOSITY GOES A LONG WAY',U.searchTitle,U.searchHint)}<div class="search-layout"><div><label class="search-box">${icon('search')}<input id="search-input" class="focusable" value="${escape(state.query)}" placeholder="${escape(U.searchPlaceholder)}" aria-label="${escape(U.searchPlaceholder)}"></label><div class="recent-searches"><span>${escape(C.sidebarPages.searchHistory)}</span><div>${(state.searchesCleared?[]:C.searches.slice(0,3)).map(label=>button(label,`query:${label}`,'chip')).join('')}</div></div><div id="search-suggestions"></div></div><section class="search-side"><div class="search-title-line"><h2 id="search-heading">${escape(U.popularSearch)}</h2>${button(U.filters,'sort','chip')}</div><div class="category-chips">${[['all',U.any],['video',U.videos],['creator','UP'],['show',C.nav.series],['live',C.nav.live]].map(([id,label])=>button(label,`searchtype:${id}`,`chip ${state.searchKind===id?'selected':''}`)).join('')}</div><div id="search-results" class="page-grid"></div></section></div>`;
  }
  function updateSearch() {
    const root = $('#search-results'); if(!root) return;
    const query = state.query.toLowerCase().trim();
    let videos = query ? allVideos.filter(v=>`${v.title} ${v.creator} ${v.category}`.toLowerCase().includes(query)) : allVideos;
    videos=explorePages.filterSearch(videos);
    explorePages.updateSuggestions();
    if(state.searchKind === 'show') videos=videos.filter(v=>C.shows.includes(v));
    if(state.searchKind === 'video')videos=videos.filter(v=>!C.shows.includes(v)&&!v.live);
    if(state.searchKind === 'live')videos=videos.filter(v=>v.live);
    if(state.searchKind === 'creator') {
      root.innerHTML=[...new Map(videos.map(v=>[v.creator,v])).values()].map(v=>`<button class="creator-result focusable" data-action="creator:${v.id}"><img src="${asset(v.image)}" alt=""><strong>${escape(v.creator)}</strong><span>${escape(U.creatorVideos)}</span></button>`).join('') || empty(U.noResults,U.noResultsHint);
    } else {
      if(state.sort === 1) videos.reverse();
      if(state.sort === 2) videos.sort((a,b)=>parseFloat(b.views||0)-parseFloat(a.views||0));
      root.innerHTML = videos.slice(0,8).map(v=>card(v,{live:state.searchKind==='live'})).join('') || empty(U.noResults,U.noResultsHint);
    }
    $('#search-heading').textContent = query ? `${U.searchResults} · ${videos.length}` : U.popularSearch;
  }
  function renderPage(restore = '') {
    const pages={home,following,series,live,library:libraryPages.render,search,messages:communityPages.messages,collection:explorePages.collection,index:explorePages.index,calendar:explorePages.calendar,settings:sidebarPages.settings,account:sidebarPages.account,entries:pluginPages.renderEntries};
    $('#main').innerHTML=`<div class="page ${state.page}-page">${viewStates.render()||(pages[state.page]||home)()}</div>`;
    renderRail(); updateSearch();
    if(restore) focusAction(restore);
  }
  function navigate(page,options={}) {
    if(!pluginPages.model.currentEntry&&!['settings','entries'].includes(page))page='entries';
    if(state.page==='settings'&&sidebarPages.preferences.section==='plugins')pluginPages.leave();
    clearTimeout(state.toastTimer);$('#toast').classList.remove('visible');
    state.returnPage=options.parent||null;state.parentFocus=options.parentFocus||null;state.returnFocus=null;state.page=page;state.category=U.any;state.overlay=null;state.detail=null;state.player=null;
    $('#overlay').innerHTML=''; $('#main').scrollTop=0; renderPage();
    requestAnimationFrame(()=>{
      const preferred=options.focus||(page==='account'?'side-login':'');
      if(preferred)focusAction(preferred);
      else $('#main .focusable')?.focus({preventScroll:true});
    });
  }
  function focusAction(action) {
    const scope=$('#system-input-layer')||$('.state-panel')||$('.branch-choice')||$('.live-state-card')||$('.playback-state-card')||$('.playback-popover')||$('.player.has-chapter-picker .chapter-picker')||$('.player.has-panel .player-panel')||$('.player')||$('.detail')||$('#stage');
    const element=$$('[data-action]',scope).find(el=>el.dataset.action===action);
    if(element) element.focus({preventScroll:true});
  }
  function showToast(text) {
    clearTimeout(state.toastTimer);$('#toast').textContent=text;$('#toast').classList.add('visible');
    state.toastTimer=setTimeout(()=>$('#toast').classList.remove('visible'),2600);
  }
  function saveLater(id) {
    const adding=!state.later.has(id);
    adding?state.later.add(id):state.later.delete(id);
    $$(`[data-action="later:${id}"]`).forEach(el=>{el.innerHTML=icon(adding?'check':'plus');el.setAttribute('aria-pressed',String(adding));});
    if(state.page==='library'&&!state.detail) renderPage();
    showToast(adding?U.saved:U.removed);
  }
  function comments() { return discussion.render('detail'); }
  function chapters(v) {
    const custom=playerFeatures.episodes(v);if(custom)return custom;
    const isShow=C.shows.some(item=>item.id===v.id);
    return `<div class="episode-grid">${(isShow ? Array.from({length:8},(_,i)=>`${String(i+1).padStart(2,'0')} / ${C.chapters[i%4]}`) : C.chapters).map((title,i)=>button(`${String(i+1).padStart(2,'0')}   ${title}`,`chapter:${i}`,'episode', '',`data-chapter="${i}"`)).join('')}</div>`;
  }
  function detailEpisodes(v) {
    return v.collection?.length>1?v.collection:playerFeatures.episodeData(v)||[];
  }
  function detailEpisodeIndex(v) {
    return v.collection?.length>1?v.collection.findIndex(episode=>episode.id===v.id):state.detailPart;
  }
  function renderDetailEpisodes(v) {
    const episodes=detailEpisodes(v);
    if(episodes.length<2)return `<p class="detail-no-episodes">${escape(U.detailNoEpisodes)}</p>`;
    const current=detailEpisodeIndex(v);
    const available=v.episodeCount||episodes.length;
    const rows=episodes.map((episode,i)=>{
      const selected=i===current;
      const title=typeof episode==='string'?episode:episode.title;
      return `<button class="episode focusable" data-action="detail-episode:${i}" aria-pressed="${selected}" ${i>=available?'disabled':''}><b>${String(i+1).padStart(2,'0')}</b><span>${escape(title)}</span>${icon(i>=available?'lock':selected?'check':'play')}</button>`;
    });
    return `<div class="episode-list-heading"><span>${escape(U.detailEpisodes)}</span><small>${available} / ${episodes.length}</small></div><div class="episode-grid">${rows.join('')}</div>`;
  }
  function openDetail(id,fromEpisode=false) {
    const focus=document.activeElement?.dataset.action;
    if(fromEpisode){
      state.detailHistory.push({id:state.detail,tab:state.detailTab,part:state.detailPart,focus,position:$('.detail').scrollTop});
    }else{
      state.detailHistory=[];
      if(state.overlay==='creator')state.creatorReturn={id:communityPages.model.creator,tab:communityPages.model.creatorTab,focus};
      state.returnFocus=focus;
    }
    if(find(id).live){startPlayer(id);if(state.player===id)openPanel('description');return;}
    state.detail=id;state.detailTab='about';state.detailPart=0;state.overlay='detail';renderDetail();
    focusAction(`play:${id}`);
  }
  function renderDetail() {
    const v=find(state.detail);const tracked=state.tracked.has(v.id);const isShow=C.shows.includes(v);
    const episodes=detailEpisodes(v);
    const episode=episodes.length>1?episodes[detailEpisodeIndex(v)]:undefined;
    $('#overlay').innerHTML=`<article class="detail"><div class="detail-bg" style="background-image:url('${asset(v.image)}')"></div><div class="detail-copy"><div class="eyebrow">${escape(v.category)} / ${isShow?'SERIES':'CREATOR’S CUT'}</div><h1>${escape(v.short||v.title)}</h1>${meta({...v,duration:episode?.duration||v.duration})}<div class="creator-line"><img src="${asset(v.image)}" alt=""><button class="focusable" data-action="creator:${v.id}">${escape(v.creator)}</button>${button(state.following.has(v.creator)?U.followed:U.follow,`follow:${v.id}`,'focusable')}</div><p class="hero-description">${escape(v.description||U.seriesSub).replace(/\n/g,'<br>')}</p><div class="actions">${button(contentAccess.label(v),`play:${v.id}`,'primary',contentAccess.kind(v)?'lock':'play')}${button(U.later,`later:${v.id}`,'secondary',state.later.has(v.id)?'check':'plus')}${isShow?button(tracked?U.seasonFollowed:U.seasonFollow,`track:${v.id}`,'secondary',tracked?'check':'plus'):button(U.triple,'triple','secondary','like')}</div><div class="detail-stats"><span>${escape(v.views||'32.6')}${escape(U.playCount)}</span><span>${escape(U.commentCount)}</span><span>2026.11.16</span></div></div><div class="detail-body"><div class="tabs">${[['about',U.about],['episodes',U.detailEpisodes],['comments',U.comments],['related',U.related]].map(([id,label])=>button(label,`detailtab:${id}`,`tab ${state.detailTab===id?'selected':''}`)).join('')}</div><div id="detail-content" class="detail-content">${detailContent(v)}</div></div></article>`;
  }
  function detailContent(v) {
    if(state.detailTab==='comments')return comments();
    if(state.detailTab==='episodes')return renderDetailEpisodes(v);
    if(state.detailTab==='related')return `<div class="cards">${C.videos.map(item=>card(item)).join('')}</div>`;
    const episodes=detailEpisodes(v);
    const episode=episodes.length>1?episodes[detailEpisodeIndex(v)]:undefined;
    const title=typeof episode==='string'?episode:episode?.title;
    return `<div class="about-grid"><div><p>${escape(v.description||U.seriesSub).replace(/\n/g,'<br>')}</p><div class="actions">${button(U.like,'like','secondary','like')}${button(U.coin,'coin','secondary','coin')}${button(U.favorite,'favorite','secondary','star')}</div></div><div><div class="mini-label">${escape(episode?U.detailEpisodes:U.detailCurrentVideo)}</div><p>${escape(title||v.short||v.title)}<br><small>${escape(episode?.duration||v.duration)}</small></p></div></div>`;
  }
  function startPlayer(id,options={}) {
    if(!state.overlay)state.returnFocus=document.activeElement?.dataset.action;
    if(state.scenario==='region'){rightsNotice('region',id);return;}
    if(find(id).interactive||find(id).parts)options={resumePosition:0,...options};
    const authorization=contentAccess.authorize(id,options);if(!authorization)return;
    state.scrub=null;state.player=id;state.overlay='player';state.position=authorization.position;state.playing=true;state.panel=null;
    if(!find(id).live)state.history=[id,...state.history.filter(item=>item!==id)].slice(0,20);
    const v=find(id);const liveMode=!!v.live;
    const preferred=sidebarPages.preferences.quality;if(preferred!==C.sidebarPages.settings.auto&&qualityAccess(preferred)==='available')state.quality=preferred;
    $('#overlay').innerHTML=`<div class="player ${state.danmaku?'':'no-danmaku'} ${liveMode?'live-player':''}"><div class="player-canvas"><img class="play-scene" src="${asset(v.image)}" alt=""><div class="danmaku">${sidebarPages.filterDanmaku(C.danmakuFixtures[v.id]||C.danmaku).map(text=>`<span>${escape(text)}</span>`).join('')}</div><div class="player-vignette"></div>${contentAccess.banner()}<div class="player-top"><div><h2>${escape(v.short||v.title)}</h2><div class="meta"><span>${escape(v.creator)}</span><i class="dot"></i><span>${liveMode?'LIVE':escape(v.category)}</span></div></div><span class="badge">${liveMode?'LIVE':state.quality}</span></div><div class="player-controls"><div class="seek-preview"><img src="${asset(v.image)}" alt=""><p><span>09:02</span><span>${escape(C.chapters[1])}</span></p></div><div class="chapter-label"><span id="chapter-name">${escape(U.chapter)}</span><span id="player-time">09:02 / ${escape(v.duration)}</span></div><button class="timeline focusable" data-action="seek" aria-label="${U.timeline}"><i class="fill"></i></button><div class="control-row"><div class="player-author"><img src="${asset(v.image)}" alt=""><div>${escape(v.creator)}<small>${escape(U.description)}</small></div>${button('','panel:description','control-button','info',`aria-label="${U.description}"`)}</div><div class="control-cluster play-center">${button('','previous','control-button','prev',`aria-label="${U.prev}"`)}${button('','pause','control-button','pause',`aria-label="Play / Pause"`)}${button('','next','control-button','next',`aria-label="${U.next}"`)}</div><div class="control-cluster">${button('','like','control-button','like',`aria-label="${U.like}"`)}${button('','panel:comments','control-button','comment',`aria-label="${U.comments}"`)}${button('','favorite','control-button','star',`aria-label="${U.favorite}"`)}${button(U.danmaku,'panel:danmaku','control-button text-control')}${button('','panel:subtitles','control-button','caption',`aria-label="${U.subtitles}"`)}${button(state.quality,'panel:quality','control-button text-control')}${button(state.speed||'1.0x','panel:speed','control-button text-control')}</div></div></div><div class="subtitle-line"></div></div><aside class="player-panel"></aside></div>`;
    playerFeatures.decorate(v);
    const comments=$('.danmaku');comments.style.opacity=String((state.dmOpacity??75)/100);if(state.dmFontSize)$$('span',comments).forEach(el=>el.style.fontSize=state.dmFontSize+'px');
    if(state.dmDensity)$$('span',comments).forEach((el,index)=>el.style.display=index<Math.ceil(state.dmDensity/20)?'':'none');
    if(state.dmArea){const height=state.dmArea==='full'?650:state.dmArea==='top'?120:300;comments.style.height=height+'px';$$('span',comments).forEach((el,index)=>el.style.top=(index*(height-35)/4)+'px');}
    $('.subtitle-line').textContent=state.subtitles===U.off?'':state.subtitles===U.english?'Somewhere beyond the familiar, a new story begins.':U.subtitleSample;
    focusAction('pause');resetHide();updateTime();
  }
  function durationSeconds(v) {return playerFeatures.duration(v).split(':').map(Number).reduce((total,n)=>total*60+n,0);}

  function updateTime() {
    if(!state.player)return;const v=find(state.player);if(v.live)return;const total=durationSeconds(v)||1936;
    state.position=Math.min(state.position,state.trial?Math.min(total,state.trial.limit):total);const percentage=state.position/total*100;
    const timestamp=`${String(Math.floor(state.position/60)).padStart(2,'0')}:${String(Math.floor(state.position%60)).padStart(2,'0')}`;
    const fill=$('.timeline .fill');if(fill)fill.style.width=`${percentage}%`;
    if($('#player-time'))$('#player-time').textContent=`${timestamp} / ${playerFeatures.duration(v)}`;
    if($('.seek-preview p span'))$('.seek-preview p span').textContent=timestamp;
    const preview=$('.seek-preview');if(preview)preview.style.setProperty('--seek-position',`${percentage}%`);
    if(!state.scrub)contentAccess.tick();playerFeatures.tick();
    const currentChapter=v.chapters?.filter(item=>item.at<=state.position).at(-1)?.title||v.short||v.title;
    if($('#chapter-name'))$('#chapter-name').textContent=currentChapter;
    if($('.seek-preview p span:last-child'))$('.seek-preview p span:last-child').textContent=currentChapter;
    playerFeatures.syncChapters();
  }
  function resetHide() {
    clearTimeout(state.autoHide);if(['branch','live-status','playback-status'].includes(state.panel))return;$('.player')?.classList.remove('hidden-controls');
    state.autoHide=setTimeout(()=>{if(!state.panel&&state.playing)$('.player')?.classList.add('hidden-controls');},6500);
  }
  function togglePause() {
    if(state.trialEnded){contentAccess.show(state.player,true);return;}
    state.playing=!state.playing;$('.player')?.classList.toggle('paused',!state.playing);
    const pause=$('[data-action="pause"]');if(pause)pause.innerHTML=icon(state.playing?'pause':'play');resetHide();
  }
  function openPanel(type) {
    if(type==='chapters'&&playerFeatures.openChapters())return;
    if(state.panel==='chapter-picker')playerFeatures.closePanel();
    if(state.player&&find(state.player).live&&playerFeatures.handle('panel:'+type))return;
    if(['quality','speed','subtitles','danmaku'].includes(type))return openPlaybackMenu(type);
    $('.playback-popover')?.remove();
    state.panel=type;$('.player').classList.add('has-panel');resetHide();
    $('.player-panel').classList.toggle('comments-panel',type==='comments');
    if(type==='comments'){discussion.reset('panel');$('.player-panel').innerHTML=discussion.render('panel');requestAnimationFrame(()=>focusAction('discussion-sort:'+discussion.model.order));return;}
    const names={description:U.description,comments:U.viewComments,quality:U.quality,subtitles:U.subtitles,speed:U.speed,danmaku:U.danmaku,chapters:U.episodes,queue:U.queue};
    let content='';
    if(type==='comments')content=`<p class="panel-subtitle">${escape(U.commentsSubtitle)}</p>${comments()}`;
    if(type==='description')content=`<p class="panel-subtitle">${escape(find(state.player).creator)}</p><p class="panel-description">${escape(find(state.player).description||U.seriesSub)}</p>${button(U.episodes,'panel:chapters','secondary','following')}<div style="height:30px"></div>${chapters(find(state.player))}`;
    if(type==='chapters')content=`<p class="panel-subtitle">CHAPTERS & EPISODES</p>${chapters(find(state.player))}`;
    if(type==='quality')content=qualityOptions();
    if(type==='subtitles')content=options([U.off,U.chinese,U.english],state.subtitles,'subtitle');
    if(type==='speed')content=options(['0.5x','0.75x','1.0x','1.25x','1.5x','2.0x'],state.speed||'1.0x','speed');
    if(type==='danmaku')content=`${options([U.on,U.off],state.danmaku?U.on:U.off,'danmaku')}<div style="height:28px"></div>${[[U.danmakuDensity,U.densityValue],[U.danmakuOpacity,U.opacityValue],[U.danmakuArea,U.areaValue]].map(([label,value])=>button(`${label} · ${value}`,`danmakuset:${label}`,'option')).join('')}`;
    $('.player-panel').innerHTML=`<div class="panel-back"><h3>${escape(names[type])}</h3></div>${content}`;
    requestAnimationFrame(()=>$('.player-panel button')?.focus());
  }

  function openPlaybackMenu(type) {
    state.panel=type;$('.player').classList.remove('has-panel');$('.playback-popover')?.remove();resetHide();
    const titles={quality:U.quality,speed:U.speed,subtitles:U.subtitles,danmaku:U.danmaku};
    let body='';
    if(type==='quality')body=qualityOptions();
    if(type==='speed')body=options(['0.5x','0.75x','1.0x','1.25x','1.5x','2.0x'],state.speed||'1.0x','speed');
    if(type==='subtitles')body=options([U.off,U.chinese,U.english],state.subtitles,'subtitle');
    if(type==='danmaku')body=`${options([U.on,U.off],state.danmaku?U.on:U.off,'danmaku')}<div class="danmaku-sliders">${[['density',M.density,state.dmDensity??60],['opacity',M.opacity,state.dmOpacity??75]].map(([id,label,value])=>`<label><span>${escape(label)}<b id="dm-${id}-value">${value}%</b></span><input class="focusable" type="range" min="10" max="100" step="${id==='opacity'?5:10}" value="${value}" data-dm-setting="${id}" aria-label="${escape(label)}"></label>`).join('')}</div>`;
    if(type==='danmaku')body+=button(C.playerFeatures.sendDanmaku,'player-compose:danmaku','quick-compose','comment');
    $('.player').insertAdjacentHTML('beforeend',`<section class="playback-popover menu-${type}" role="dialog" aria-label="${escape(titles[type])}"><header><h3>${escape(titles[type])}</h3><span>${type==='speed'?escape(state.speed||'1.0x'):type==='quality'?escape(state.quality):''}</span></header><div class="playback-menu-body">${body}</div></section>`);
    requestAnimationFrame(()=>{const selected=$('.playback-popover .selected')||$('.playback-popover .option')||$('.playback-popover button');selected?.focus({preventScroll:true});});
  }
  function qualityAccess(value) {
    if(value.startsWith('4K')&&state.scenario==='source')return 'source';
    if(value.includes('HDR')&&state.scenario==='device')return 'device';
    if((value.startsWith('4K')||value==='1080P 60')&&(!state.account||state.scenario==='expired'))return 'login';
    if((value.startsWith('4K')||value==='1080P 60')&&!state.member)return 'vip';
    return 'available';
  }
  function qualityOptions() {
    return `<p class="panel-subtitle">${escape(R.scenes[state.scenario]||R.scenes.free)}</p><div class="option-list">${['4K HDR','4K','1080P 60','1080P','720P',U.auto].map(value=>{const access=qualityAccess(value);return `<button class="focusable option ${state.quality===value?'selected':''}" data-action="quality:${value}"><span>${escape(value)}<small>${escape(['vip','login','device','source'].includes(access)?R[access]:'')}</small></span>${state.quality===value?icon('check'):access==='vip'||access==='login'?icon('lock'):access==='device'||access==='source'?icon('info'):''}</button>`;}).join('')}</div>`;
  }
  function applyQuality(value) {
    state.quality=value;const badge=$('.player-top .badge');if(badge)badge.textContent=value;
    const trigger=$('[data-action="panel:quality"]');if(trigger)trigger.textContent=value;
    if(state.player&&state.panel==='quality')closePanel();showToast(`${U.selected} ${value}`);
  }
  function selectQuality(value) {
    const access=qualityAccess(value);state.pendingQuality=value;
    if(access==='available'){state.pendingQuality=null;applyQuality(value);return;}
    if(access==='login'){login();return;}
    rightsNotice(access);
  }
  function rightsNotice(kind) {
    if(kind==='vip'){
      modal(R.upgradeTitle,`<div class="rights-kicker">${icon('sparkles')}<span>${escape(R.vip)}</span></div><p>${escape(R.upgradeBody)}</p><div class="rights-phone" hidden><p>${escape(R.phoneBody)}</p><span class="official-link">${escape(C.access.official)}</span></div>`,button(R.phone,'rightsphone','primary')+button(R.refresh,'rightsrefresh','secondary')+button(R.keep,'dismissmodal','quiet-action'));
    }else if(kind==='device'){
      modal(R.deviceTitle,`<p>${escape(R.deviceBody)}</p>`,button(R.useSdr,'qualityfallback:4K','primary')+button(R.keep,'dismissmodal','secondary'));
    }else if(kind==='source'){
      modal(R.sourceTitle,`<p>${escape(R.sourceBody)}</p>`,button(R.useBest,'qualityfallback:1080P','primary')+button(U.close,'dismissmodal','secondary'));
    }else if(kind==='region'){
      modal(R.regionTitle,`<p>${escape(R.regionBody)}</p>`,button(R.seeRelated,'relatedfallback','primary')+button(U.close,'dismissmodal','secondary'));
    }
  }
  function setScenario(value) {
    if(value==='vip-content'||value==='purchase-content'){if(state.overlay==='modal')dismissModal();state.scenario='free';state.member=false;state.account=true;state.player=null;state.panel=null;openDetail(value==='vip-content'?'show-4':'show-2');return;}
    if(value==='trial-end'){if(state.overlay==='modal')dismissModal();contentAccess.previewEnd();return;}
    if(state.overlay==='modal')dismissModal();
    state.scenario=value;state.member=['member','device','source','region'].includes(value);state.account=value!=='guest';contentAccess.promote();
    if(state.player){applyQuality(state.member?'4K':value==='guest'?'720P':'1080P');if(state.panel)openPanel('quality');}
  }

  function options(values,current,action) {
    return `<div class="option-list">${values.map(value=>`<button class="focusable option ${current===value?'selected':''}" data-action="${action}:${value}"><span>${escape(value)}</span>${current===value?icon('check'):''}</button>`).join('')}</div>`;
  }
  function closePanel(){if(playerFeatures.closePanel())return;const origin=state.panel;state.panel=null;$('.playback-popover')?.remove();$('.player')?.classList.remove('has-panel');focusAction('panel:'+origin);resetHide();}
  function closeOverlay(){
    if(state.overlay==='modal'){if($('.auth-modal')){authFlow.cancel();pluginPages.cancelAccountLogin();state.pendingAction=null;state.pendingQuality=null;state.resumeContentAfterLogin=null;}dismissModal();return;}
    if(state.panel){closePanel();return;}
    clearTimeout(state.autoHide);
    if(state.overlay==='modal'&&state.modalReturn){const parent=state.modalReturn;state.modalReturn=null;if(parent==='player'){startPlayer(state.player);return;}if(parent==='detail'){state.overlay='detail';renderDetail();return;}}
    if(state.player&&state.detail){state.player=null;state.overlay='detail';renderDetail();focusAction(`play:${state.detail}`);return;}
    if(state.overlay==='detail'&&state.detailHistory.length){
      const origin=state.detailHistory.pop();
      state.detail=origin.id;state.detailTab=origin.tab;state.detailPart=origin.part;
      renderDetail();$('.detail').scrollTop=origin.position;focusAction(origin.focus);
      return;
    }
    if(state.overlay==='detail'&&!state.player&&state.creatorReturn){const origin=state.creatorReturn;state.creatorReturn=null;communityPages.openCreator(origin.id,origin);return;}
    state.overlay=null;state.player=null;state.detail=null;$('#overlay').innerHTML='';
    focusAction(state.returnFocus||(['settings','account'].includes(state.page)?state.page:`nav:${state.page}`));
  }
  function modal(title,body,actions='') {
    clearTimeout(state.toastTimer);$('#toast').classList.remove('visible');state.modalFocus=document.activeElement;state.modalReturn=state.overlay;state.overlay='modal';
    $('#overlay').insertAdjacentHTML('beforeend',`<div class="state-panel"><section class="modal" role="dialog" aria-modal="true" tabindex="-1"><h2>${escape(title)}</h2>${body}<div class="actions">${actions}</div></section></div>`);
    requestAnimationFrame(()=>($('.state-panel .preference-option.focusable.selected:not(:disabled)')||$('.state-panel .preference-option.focusable:not(:disabled)')||$('.state-panel .actions button:not(:disabled)')||$('.state-panel .focusable:not(:disabled)')||$('.state-panel .modal'))?.focus({preventScroll:true}));
  }
  function dismissModal(){
    $('.state-panel')?.remove();state.overlay=state.modalReturn;state.modalReturn=null;
    if(state.modalFocus?.isConnected)state.modalFocus.focus({preventScroll:true});else if(state.player)focusAction('pause');else if(state.detail)focusAction(`play:${state.detail}`);else focusAction(['settings','account'].includes(state.page)?state.page:`nav:${state.page}`);
  }
  function login(){authFlow.open();}

  function creator(id) {communityPages.openCreator(id);}

  function triple() {
    const id=state.player||state.detail||'orbit';state.likes.add(id);state.favorites.add(id);libraryPages.saveToDefault(id);
    $('#stage').insertAdjacentHTML('beforeend',`<div class="celebration"><div class="triple-icons">${icon('like')}${icon('coin')}${icon('star')}</div><p>${escape(U.tripleDone)}</p></div>`);
    setTimeout(()=>$('.celebration')?.remove(),2000);
  }
  function perform(action,element) {
    if(!action)return;const [verb,...rest]=action.split(':');const value=rest.join(':');
    if(themePages.handle(action)||pluginPages.handle(action))return;
    if(['like','coin','favorite','triple','follow','track','discussion-vote','discussion-compose','discussion-send','community-follow','community-compose','community-send','player-compose','player-send-message','player-live-follow'].includes(verb)&&(!state.account||state.scenario==='expired')){state.pendingAction=action;login();return;}
    if(viewStates.handle(action,element))return;
    if(playerFeatures.handle(action,element))return;
    if(communityPages.handle(action,element))return;
    if(textInput.handle(action,element))return;
    if(authFlow.handle(action,element))return;
    if(explorePages.handle(action,element))return;
    if(libraryPages.handle(action,element))return;
    if(verb.startsWith('side-')&&sidebarPages.handle(action,element))return;
    if(verb==='following-view'){state.followingView=value;renderPage(action);return;}
    if(verb.startsWith('content-')&&contentAccess.handle(action,element))return;
    if(verb.startsWith('discussion-')&&discussion.handle(action))return;
    if(verb==='nav')return navigate(value);
    if(verb==='tab'){state.homeTab=value;state.category=U.any;renderPage(`tab:${value}`);return;}
    if(verb==='hero'){state.hero=Number(value);renderPage(`hero:${value}`);return;}
    if(verb==='recommendations-refresh'){
      const position=$('#main').scrollTop;
      state.recommendationOffset=((state.recommendationOffset||0)+1)%(C.videos.length+C.heroes.length);
      renderPage(action);
      $('#main').scrollTop=position;
      return;
    }
    if(verb==='home-recommendation')return openDetail(value);
    if(verb==='detail')return openDetail(value);
    if(verb==='play')return startPlayer(value);
    if(verb==='creator')return creator(value);
    if(verb==='later')return saveLater(value);
    if(verb==='category'){state.category=value;renderPage(`category:${value}`);return;}
    if(verb==='library'){state.libraryTab=value;renderPage(`library:${value}`);return;}
    if(verb==='detail-episode'){
      const video=find(state.detail),index=Number(value);
      const episode=detailEpisodes(video)[index];
      if(!episode)return;
      if(episode.id&&episode.id!==video.id){openDetail(episode.id,true);return;}
      if(video.collection?.length>1)return;
      state.detailPart=index;
      $('.detail .meta').outerHTML=meta({...video,duration:episode.duration||video.duration});
      $('#detail-content').innerHTML=renderDetailEpisodes(video);
      focusAction(action);
      return;
    }
    if(verb==='detailtab'){
      if(value==='comments')discussion.reset('detail');
      state.detailTab=value;renderDetail();focusAction(action);
      const detail=$('.detail');
      const bounds=detail.getBoundingClientRect();
      const scale=bounds.height/detail.clientHeight;
      const tabTop=($('.detail .tabs').getBoundingClientRect().top-bounds.top)/scale;
      detail.scrollTo({top:value==='comments'?Math.max(0,tabTop-125):420,behavior:state.reduced?'instant':'smooth'});
      return;
    }
    if(verb==='follow'){const v=find(value);const isFollowed=state.following.has(v.creator);isFollowed?state.following.delete(v.creator):state.following.add(v.creator);element.textContent=isFollowed?U.follow:U.followed;showToast(isFollowed?U.follow:U.followed);return;}
    if(verb==='track'){state.tracked.has(value)?state.tracked.delete(value):state.tracked.add(value);if(state.detail)renderDetail();else renderPage();focusAction(action);return;}
    if(verb==='panel')return openPanel(value);
    if(verb==='closepanel')return closePanel();
    if(verb==='pause')return togglePause();
    if(verb==='close')return closeOverlay();
    if(verb==='dismissmodal')return dismissModal();
    if(verb==='quality')return selectQuality(value);
    if(verb==='qualityfallback'){dismissModal();applyQuality(value);return;}
    if(verb==='rightsphone'){ $('.rights-phone').hidden=false;return;}
    if(verb==='rightsrefresh'){element.textContent=R.refreshing;element.disabled=true;setTimeout(()=>{state.member=true;state.account=true;contentAccess.promote();state.scenario='member';$('#scenario-select').value='member';const desired=state.pendingQuality||'4K';dismissModal();applyQuality(desired);if(state.page==='account'&&!state.player)renderPage('side-membership');showToast(R.refreshed);},900);return;}
    if(verb==='relatedfallback'){dismissModal();state.scenario='free';$('#scenario-select').value='free';navigate('home');showToast(U.related);return;}
    if(verb==='likeinstead'){dismissModal();state.likes.add(state.player||state.detail);showToast(U.liked);return;}
    if(verb==='subtitle'){state.subtitles=value;$('.subtitle-line').textContent=value===U.off?'':value===U.english?'Somewhere beyond the familiar, a new story begins.':U.subtitleSample;closePanel();return;}
    if(verb==='speed'){state.speed=value;$('[data-action="panel:speed"]').textContent=value;closePanel();return;}
    if(verb==='danmaku'){state.danmaku=value===U.on;$('.player').classList.toggle('no-danmaku',!state.danmaku);openPanel('danmaku');return;}
    if(verb==='danmakuset'){element.classList.toggle('selected');showToast(value+' · '+U.selected);return;}
    if(verb==='chapter'){if(!state.player)startPlayer(state.detail);state.position=Number(value)*450;updateTime();closePanel();return;}
    if(verb==='seek'){if(element&&state.clickX!==null){const bounds=element.getBoundingClientRect();const x=state.clickX;state.position=(x-bounds.left)/bounds.width*durationSeconds(find(state.player));}updateTime();return;}
    if(verb==='previous'||verb==='next'){if(playerFeatures.advance(verb==='next'?1:-1))return;const queue=allVideos.filter(v=>!v.live);const index=queue.findIndex(v=>v.id===state.player);startPlayer(queue[(index+(verb==='next'?1:queue.length-1))%queue.length].id);return;}
    if(verb==='like'){const id=state.player||state.detail;state.likes.has(id)?state.likes.delete(id):state.likes.add(id);element.classList.toggle('liked-heart',state.likes.has(id));showToast(state.likes.has(id)?U.liked:U.like);return;}
    if(verb==='favorite'){const id=state.player||state.detail;const adding=!state.favorites.has(id);adding?state.favorites.add(id):state.favorites.delete(id);element.classList.toggle('liked-heart',adding);showToast(adding?U.favoriteAdded:U.favoriteRemoved);return;}
    if((verb==='coin'||verb==='triple')&&(state.scenario==='coins'||state.coins===0)){modal(R.coinTitle,`<p>${escape(R.coinBody)}</p>`,button(R.likeInstead,'likeinstead','primary','like')+button(U.close,'dismissmodal','secondary'));return;}
    if(verb==='coin')return modal(U.confirmCoin,`<p>${escape(U.coinNote)}</p>`,button(U.confirm,'confirmcoin','primary','coin')+button(U.cancel,'dismissmodal','secondary'));
    if(verb==='confirmcoin'){state.coins=Math.max(0,state.coins-1);dismissModal();showToast(U.coinDone);return;}
    if(verb==='triple')return modal(U.confirmCoin,`<p>${escape(U.coinNote)}</p>`,button(U.triple,'confirmtriple','primary','like')+button(U.cancel,'dismissmodal','secondary'));
    if(verb==='confirmtriple'){state.coins=Math.max(0,state.coins-1);dismissModal();triple();return;}
    if(verb==='settings')return navigate('settings');
    if(verb==='account')return navigate('account');
    if(verb==='login'){state.account=true;pluginPages.completeAccountLogin();state.scenario=state.member?'member':'free';$('#scenario-select').value=state.scenario;dismissModal();if(state.resumeContentAfterLogin){const resume=state.resumeContentAfterLogin;state.resumeContentAfterLogin=null;startPlayer(resume);return;}if(state.pendingQuality){const desired=state.pendingQuality;state.pendingQuality=null;selectQuality(desired);}else if(state.pendingAction){const pending=state.pendingAction;state.pendingAction=null;const target=$$('[data-action]').find(el=>el.dataset.action===pending);if(target)perform(pending,target);}else if(!state.player&&!state.detail)navigate('account');return;}
    if(verb==='setting'){
      if(value==='motion'){state.reduced=!state.reduced;$('#stage').classList.toggle('reduced-motion',state.reduced);}
      if(value==='preview'){state.preview=!state.preview;$('.hero-scene')?.style.setProperty('animation-play-state',state.preview?'running':'paused');}
      if(value==='defaultdanmaku')state.danmaku=!state.danmaku;
      element.querySelector('.switch').classList.toggle('on');return;
    }
    if(verb==='preferred'){const qualities=['4K HDR','4K','1080P',U.auto];state.quality=qualities[(qualities.indexOf(state.quality)+1)%qualities.length];element.textContent=U.preferredQuality+' · '+state.quality;return;}
    if(verb==='reset'){state.later=new Set(['mountains','forest']);state.favorites=new Set(['orbit','city']);dismissModal();navigate('home');return;}
    if(verb==='states'){dismissModal();modal(U.loadState,`<div class="settings-list">${[U.loading,U.networkError,U.loginExpired].map((label,i)=>button(label,`state:${i}`,'setting-row')).join('')}</div>`);return;}
    if(verb==='state'){dismissModal();if(value==='0'){modal(U.loading,'<div class="skeletons"><i></i><i></i><i></i></div>');setTimeout(()=>{$('.state-panel')&&dismissModal();},2200);}else if(value==='1'){modal(U.networkTitle,`<p>${escape(U.networkSub)}</p>`,button(U.retry,'dismissmodal','primary'));}else{modal(U.expiredTitle,`<p>${escape(U.expiredSub)}</p>`,button(U.loginButton,'login','primary'));}return;}
    if(verb==='query'){state.query=value;$('#search-input').value=value;updateSearch();return;}
    if(verb==='key'){state.query=value==='delete'?state.query.slice(0,-1):state.query+(value==='space'?' ':value);$('#search-input').value=state.query;updateSearch();return;}
    if(verb==='searchtype'){state.searchKind=value;$$('.search-side .chip[data-action^="searchtype:"]').forEach(el=>el.classList.toggle('selected',el.dataset.action===action));updateSearch();return;}
    if(verb==='sort'){state.sort=(state.sort+1)%3;element.textContent=[U.filters,U.sortNew,U.sortViews][state.sort];updateSearch();return;}
    if(verb==='all'){state.homeTab='categories';navigate('home');state.homeTab='categories';renderPage();focusAction('tab:categories');return;}
  }
  function visibleElements() {
    const scope=$('#system-input-layer')||$('.state-panel')||$('.branch-choice')||$('.live-state-card')||$('.playback-state-card')||$('.playback-popover')||$('.player.has-chapter-picker .chapter-picker')||$('.player.has-panel .player-panel')||$('.player')||$('.detail')||$('#stage');
    return $$('.focusable',scope).filter(el=>{const r=el.getBoundingClientRect();return !el.disabled&&!el.closest('[inert]')&&r.width&&r.height&&getComputedStyle(el).visibility!=='hidden';});
  }
  function moveFocus(direction) {
    const current=document.activeElement;const elements=visibleElements();
    if(!elements.includes(current)){elements[0]?.focus();return;}
    const horizontal=direction==='ArrowLeft'||direction==='ArrowRight';
    const recommendationRow=current.closest('.home-recommendation-row');
    if(recommendationRow&&horizontal){
      const cards=$$('.video-card',recommendationRow);
      const next=cards[cards.indexOf(current)+(direction==='ArrowRight'?1:-1)];
      if(next){
        next.focus({preventScroll:true});
        next.scrollIntoView({behavior:state.reduced?'instant':'smooth',block:'nearest',inline:'nearest'});
      }else if(direction==='ArrowLeft'){
        focusAction('nav:home');
      }
      return;
    }
    if(horizontal&&current.matches('.detail .tabs [data-action^="detailtab:"]')){
      const tabs=$$('.detail .tabs [data-action^="detailtab:"]');
      const next=tabs[tabs.indexOf(current)+(direction==='ArrowRight'?1:-1)];
      if(next)perform(next.dataset.action,next);
      return;
    }
    if(state.page==='account'&&!state.overlay){
      const action=sidebarPages.accountFocus(current.dataset.action,direction);
      if(action){focusAction(action);return;}
    }
    const rect=current.getBoundingClientRect();const origin={x:rect.left+rect.width/2,y:rect.top+rect.height/2};
    let best=null,bestScore=Infinity;
    for(const el of elements){if(el===current)continue;const r=el.getBoundingClientRect();const dx=r.left+r.width/2-origin.x;const dy=r.top+r.height/2-origin.y;
      const primary=direction==='ArrowRight'?dx:direction==='ArrowLeft'?-dx:direction==='ArrowDown'?dy:-dy;
      const cross=direction==='ArrowLeft'||direction==='ArrowRight'?Math.abs(dy):Math.abs(dx);
      if(primary<5)continue;const score=primary+cross*3.2+(cross>primary*1.4?350:0);
      if(score<bestScore){bestScore=score;best=el;}
    }
    if(best){
      const currentGroup=current.dataset.action?.split(':')[0];
      if(horizontal&&state.page==='home'&&!state.overlay&&['tab','category','rank-genre'].includes(currentGroup)&&best.dataset.action?.split(':')[0]===currentGroup){
        perform(best.dataset.action,best);
        return;
      }
      best.focus({preventScroll:true});best.scrollIntoView({behavior:state.reduced?'instant':'smooth',block:'nearest',inline:'nearest'});
    }
  }
  function handleKey(event) {
    if(textInput.handleKey(event)||pluginPages.handleKey(event)||playerFeatures.handleKey(event))return;
    if(event.key==='Escape'&&state.player&&state.overlay!=='modal'&&!state.panel){
      if(state.scrub){const previous=state.scrub;state.scrub=null;state.position=previous.origin;state.playing=previous.wasPlaying;$('.player').classList.remove('seeking');updateTime();event.preventDefault();return;}
      if(!$('.player').classList.contains('hidden-controls')){clearTimeout(state.autoHide);$('.player').classList.add('hidden-controls');document.activeElement.blur();event.preventDefault();return;}
    }
    if(event.key==='Escape'&&state.overlay!=='modal'&&(state.panel==='comments'||(state.overlay==='detail'&&state.detailTab==='comments'))&&discussion.back()){event.preventDefault();return;}
    if(event.key==='Escape'&&!state.overlay&&communityPages.back()){event.preventDefault();return;}
    if(event.key==='Escape'&&!state.overlay&&sidebarPages.back()){event.preventDefault();return;}
    if(event.key==='Escape'&&!state.overlay&&state.returnPage){event.preventDefault();if(state.page==='library'&&libraryPages.back())return;navigate(state.returnPage,{focus:state.parentFocus});return;}
    if(event.key==='Escape'&&!state.overlay&&explorePages.back()){event.preventDefault();return;}
    if(event.key==='Escape'&&!state.overlay&&libraryPages.back()){event.preventDefault();return;}
    if(event.key==='Escape'){event.preventDefault();if(state.overlay)closeOverlay();else if(state.page!=='home')navigate('home');else focusAction('nav:home');return;}
    if(event.target?.classList.contains('scroll-region')&&['ArrowUp','ArrowDown'].includes(event.key)){const el=event.target;const down=event.key==='ArrowDown';if(down?el.scrollTop+el.clientHeight<el.scrollHeight-1:el.scrollTop>0){event.preventDefault();el.scrollBy({top:down?220:-220,behavior:state.reduced?'instant':'smooth'});return;}}
    if(event.key==='Enter'&&event.target?.matches('#stage input:not([type=range]):not([type=hidden]),#stage textarea')&&event.target.id!=='system-text'){event.preventDefault();textInput.openField(event.target);return;}
    if(['INPUT','TEXTAREA'].includes(event.target?.tagName)&& !['ArrowDown','ArrowUp'].includes(event.key))return;
    if(event.key===' '&&state.player&&!state.panel&&state.overlay!=='modal'){event.preventDefault();togglePause();return;}
    if(event.key==='Enter'&&state.scrub){const previous=state.scrub;state.scrub=null;state.playing=previous.wasPlaying;$('.player').classList.remove('seeking');updateTime();event.preventDefault();return;}
    if(event.key==='Enter'&&document.activeElement?.matches('[role=button][data-action]')){event.preventDefault();perform(document.activeElement.dataset.action,document.activeElement);return;}
    if(event.key==='Enter'){if(document.activeElement===document.body){event.preventDefault();focusAction('play:orbit');}return;}
    if(!event.key.startsWith('Arrow'))return;event.preventDefault();
    if(state.player&&!state.panel){
      const wasHidden=$('.player').classList.contains('hidden-controls');resetHide();
      if(wasHidden){focusAction('pause');return;}
      if(document.activeElement?.classList.contains('timeline')&&(event.key==='ArrowLeft'||event.key==='ArrowRight')){
        if(!state.scrub){state.scrub={origin:state.position,wasPlaying:state.playing};state.playing=false;}state.position=Math.max(0,state.position+(event.key==='ArrowRight'?10:-10));updateTime();$('.player').classList.add('seeking');return;
      }
      if(event.key==='ArrowUp'&&document.activeElement?.classList.contains('timeline')){playerFeatures.openChapters();return;}
    }
    moveFocus(event.key);
  }
  document.addEventListener('click',(event)=>{const element=event.target.closest('[data-action]');if(!element)return;state.clickX=event.detail>0?event.clientX:null;perform(element.dataset.action,element);});
  document.addEventListener('input',(event)=>{if(pluginPages.onInput(event)||discussion.onInput?.(event))return;if(event.target.dataset.dmSetting){const key=event.target.dataset.dmSetting;const value=Number(event.target.value);$('#dm-'+key+'-value').textContent=value+'%';if(key==='opacity'){state.dmOpacity=value;$('.danmaku').style.opacity=value/100;}else{state.dmDensity=value;$$('.danmaku span').forEach((el,index)=>el.style.display=index<Math.ceil(value/20)?'':'none');}return;}if(event.target.id==='search-input'){state.query=event.target.value;updateSearch();}});
  document.addEventListener('keydown',handleKey);
  new ResizeObserver(()=>{
    const recommendations=$('.home-recommendations');
    if(!recommendations||Number(recommendations.dataset.columns)===recommendationColumns())return;
    const focus=recommendations.contains(document.activeElement)?document.activeElement.dataset.action:'';
    recommendations.outerHTML=homeRecommendations();
    if(focus)focusAction(focus);
  }).observe($('#main'));
  document.addEventListener('mousemove',event=>{if(state.player&&event.target.closest('.player'))resetHide();});
  document.addEventListener('focusin',(event)=>{if(event.target.classList.contains('focusable'))state.focus=event.target.dataset.action;if(event.target.closest('#main'))state.contentFocus=event.target.dataset.action;pluginPages.onFocus(event.target);playerFeatures.syncChapters();});
  $$('[data-key]').forEach(el=>el.addEventListener('click',()=>{
    const target=$$('[data-action]').find(item=>item.dataset.action===state.focus)||document.activeElement;
    target?.focus({preventScroll:true});
    const key=el.dataset.key;state.clickX=null;
    const handled=!target.dispatchEvent(new KeyboardEvent('keydown',{key,bubbles:true,cancelable:true}));
    if(key==='Enter'&&!handled&&target.matches('[data-action]'))perform(target.dataset.action,target);
  }));
  window.addEventListener('resize',resize);
  $('#view-state-select').innerHTML=Object.entries(C.viewStates.choices).map(([value,label])=>`<option value="${value}">${escape(label)}</option>`).join('');
  $('#view-state-select').addEventListener('change',event=>viewStates.set(event.target.value));
  $('#auth-state-select').innerHTML=Object.entries(C.auth.phases).map(([value,label])=>`<option value="${value}">${escape(label)}</option>`).join('');
  $('#auth-state-select').addEventListener('change',event=>authFlow.preview(event.target.value));
  $('#scenario-select').innerHTML=Object.entries(R.scenes).map(([value,label])=>`<option value="${value}">${escape(label)}</option>`).join('');
  $('#scenario-select').addEventListener('change',event=>setScenario(event.target.value));
  $('#review-label').textContent=C.review;$('#keyboard-help').textContent=C.help;
  themePages.restore();renderPage();resize();
  setInterval(()=>{if(state.player&&state.playing){state.position+=(parseFloat(state.speed)||1);updateTime();}},1000);
  const sleep=(ms)=>new Promise(resolve=>setTimeout(resolve,ms));
  async function tour() {
    if(state.tour)return;state.tour=true;state.detail=null;state.player=null;state.overlay=null;$('#overlay').innerHTML='';state.homeTab='forYou';navigate('home');document.activeElement.blur();
    await sleep(1300);focusAction('play:orbit');await sleep(1300);focusAction('detail:orbit');await sleep(800);openDetail('orbit');await sleep(2100);startPlayer('orbit');await sleep(1700);openPanel('comments');await sleep(1900);closePanel();await sleep(700);openPanel('quality');await sleep(1600);closePanel();await sleep(500);state.detail=null;state.player=null;state.overlay=null;$('#overlay').innerHTML='';navigate('following');await sleep(1800);navigate('search');state.query=C.searches[0];$('#search-input').value=state.query;updateSearch();await sleep(1600);navigate('library');await sleep(1600);navigate('home');document.activeElement.blur();state.tour=false;
  }
  $('#tour-button').addEventListener('click',tour);
  $('#fullscreen-button').addEventListener('click',()=>document.fullscreenElement?document.exitFullscreen():document.documentElement.requestFullscreen());
  document.addEventListener('fullscreenchange',()=>{document.body.classList.toggle('is-fullscreen',!!document.fullscreenElement);resize();});
  window.MikuPrototype={state,navigate,openDetail,startPlayer,openPanel,closePanel,perform,tour,updateTime,setScenario,library:libraryPages,settings:sidebarPages,themes:themePages,explore:explorePages,auth:authFlow,textInput,community:communityPages,playerFeatures,viewStates,plugins:pluginPages};
  const entry=new URLSearchParams(location.search);const entryId=entry.get('id');if(entryId&&allVideos.some(v=>v.id===entryId)){openDetail(entryId);if(entry.get('route')==='play')startPlayer(entryId);}
})();
