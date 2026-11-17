'use strict';
window.createExplorePages = ({content:C,state,icon,escape:e,button,header,card,modal,dismissModal,navigate,renderPage,updateSearch}) => {
  const X=C.explore;
  const shelfRegistry=new Map();
  const model={parent:'home',listing:null,rankGenre:C.ui.any,filters:{genre:C.ui.any,region:C.ui.any,year:C.ui.any,status:C.ui.any,sort:X.sorts[0]},day:0,searchDuration:'all'};
  const homeTabs=()=>[['forYou',C.ui.forYou],['trending',C.ui.trending],['ranking',X.ranking],['categories',C.ui.categories]];
  const title=(eyebrow,name,note)=>`<div class="page-title"><div class="eyebrow">${e(eyebrow)}</div><h1>${e(name)}</h1><p>${e(note)}</p></div>`;
  const navTools=()=>`<div class="series-tools">${button(X.index,'explore:index','text-back','search')}${button(X.calendar,'explore:calendar','text-back','history')}</div>`;
  function registerShelf(name,items,options){const key=encodeURIComponent(name);shelfRegistry.set(key,{name,items,options});return `all:${key}`;}
  function collection(){const list=model.listing;return `${header()}<div class="collection-heading">${button(X.back,'explore-back','text-back','back')}<div><h1>${e(list.name)}</h1><p>${list.items.length} ${e(X.items)}</p></div></div><div class="page-grid">${list.items.map(v=>card(v,list.options)).join('')}</div>`;}
  function ranking(){
    let items=[...C.heroes,...C.videos].sort((a,b)=>parseFloat(b.views)-parseFloat(a.views));
    if(model.rankGenre!==C.ui.any)items=items.filter(v=>v.category.includes(model.rankGenre));
    const genres=[C.ui.any,...new Set([...C.heroes,...C.videos].map(v=>v.category.split(' · ')[0]))];
    return `${header(homeTabs(),'ranking')}${title('THE STORIES EVERYONE IS WATCHING',X.ranking,X.rankingNote)}<div class="category-chips">${genres.map(label=>button(label,`rank-genre:${label}`,`chip ${model.rankGenre===label?'selected':''}`)).join('')}</div><div class="ranking-podium">${items.slice(0,3).map((v,i)=>`<div class="rank-card"><b>0${i+1}</b>${card(v)}<span>${e(X.heat)} ${[98.7,96.2,93.5][i]}</span></div>`).join('')}</div><div class="ranking-list">${items.slice(3).map((v,i)=>`<button class="focusable ranking-row" data-action="detail:${v.id}"><b>${String(i+4).padStart(2,'0')}</b><img src="assets/${v.image}" alt=""><span><strong>${e(v.short||v.title)}</strong><small>${e(v.creator)} · ${e(v.category)}</small></span><small>${e(v.views)} ${e(C.ui.playCount)}</small></button>`).join('')}</div>`;
  }
  function index(){
    const F=model.filters;
    let shows=C.shows.filter(v=>(F.genre===C.ui.any||v.category===F.genre)&&(F.region===C.ui.any||v.region===F.region)&&(F.year===C.ui.any||String(v.year)===F.year)&&(F.status===C.ui.any||v.status===F.status));
    if(F.sort===X.sorts[1])shows=shows.slice().reverse();
    if(F.sort===X.sorts[2])shows=shows.slice().sort((a,b)=>parseFloat(b.views)-parseFloat(a.views));
    return `${header()}<div class="collection-heading">${button(C.nav.series,'explore-back','text-back','back')}<div><h1>${e(X.index)}</h1><p>${e(X.indexNote)}</p></div></div><div class="index-filters">${Object.entries(F).map(([key,value])=>button(`${X.filterNames[key]} · ${value}`,`index-filter:${key}`,'chip','down')).join('')}${button(X.reset,'index-reset','text-back','refresh')}</div><div class="collection-meta"><span>${e(X.results)}</span><small>${shows.length} ${e(X.works)}</small></div>${shows.length?`<div class="page-grid index-results">${shows.map(v=>card(v,{show:true})).join('')}</div>`:`<section class="collection-empty"><span>${icon('search')}</span><h2>${e(X.noFilterResults)}</h2><p>${e(X.noFilterNote)}</p>${button(X.reset,'index-reset','secondary','refresh')}</section>`}`;
  }
  function calendar(){
    const today=new Date();today.setHours(0,0,0,0);
    const dates=Array.from({length:7},(_,i)=>new Date(today.getFullYear(),today.getMonth(),today.getDate()+i));
    const selected=dates[model.day];
    const items=C.shows.filter(v=>v.updateDay===selected.getDay()).sort((a,b)=>a.updateTime.localeCompare(b.updateTime));
    return `${header()}<div class="collection-heading">${button(C.nav.series,'explore-back','text-back','back')}<div><h1>${e(X.calendar)}</h1><p>${e(X.calendarNote)}</p></div></div><div class="calendar-days">${dates.map((date,i)=>`<button class="focusable calendar-day ${model.day===i?'selected':''}" data-action="calendar-day:${i}"><small>${e(i===0?X.today:X.weekdays[date.getDay()])}</small><b>${String(date.getDate()).padStart(2,'0')}</b><span>${date.getMonth()+1}${e(X.month)}</span></button>`).join('')}</div><div class="calendar-caption"><span>${selected.getMonth()+1}.${String(selected.getDate()).padStart(2,'0')}</span><h2>${e(X.scheduled)}</h2><small>${items.length} ${e(X.works)}</small></div>${items.length?`<div class="calendar-schedule">${items.map(v=>`<div class="schedule-row"><span class="schedule-time">${e(v.updateTime)}</span><button class="focusable schedule-show" data-action="detail:${v.id}"><img src="assets/${v.image}" alt=""><span><small>${e(v.category)}</small><strong>${e(v.title)}</strong><em>${e(X.episodePrefix)} ${v.episodeCount} ${e(X.episodeUnit)}</em></span></button>${button(state.tracked.has(v.id)?C.ui.seasonFollowed:C.ui.seasonFollow,`track:${v.id}`,'secondary',state.tracked.has(v.id)?'check':'plus')}</div>`).join('')}</div>`:`<section class="collection-empty"><h2>${e(X.noUpdates)}</h2><p>${e(X.noUpdatesNote)}</p>${button(X.index,'explore:index','secondary','search')}</section>`}`;
  }
  function filterMenu(key){
    const choices={genre:[C.ui.any,...new Set(C.shows.map(v=>v.category))],region:[C.ui.any,...new Set(C.shows.map(v=>v.region))],year:[C.ui.any,'2026','2025','2024'],status:[C.ui.any,X.ongoing,X.finished],sort:X.sorts}[key];
    modal(X.filterNames[key],`<div class="preference-options">${choices.map((label,i)=>`<button class="focusable preference-option ${model.filters[key]===label?'selected':''}" data-action="index-choice:${key}:${i}"><span>${e(label)}</span>${model.filters[key]===label?icon('check'):''}</button>`).join('')}</div>`);
    model.filterChoices=choices;
  }
  function searchMenu(){
    modal(X.searchFilters,`<h3 class="filter-subtitle">${e(X.sort)}</h3><div class="search-sort-options">${[C.ui.filters,C.ui.sortNew,C.ui.sortViews].map((label,i)=>button(label,`search-sort:${i}`,`chip ${state.sort===i?'selected':''}`)).join('')}</div><h3 class="filter-subtitle">${e(X.duration)}</h3><div class="search-duration-options">${['all','short','medium','long'].map(key=>button(X.durations[key],`search-duration:${key}`,`chip ${model.searchDuration===key?'selected':''}`)).join('')}</div>`,button(X.apply,'search-apply','primary'));
  }
  function updateSuggestions(){
    const target=document.querySelector('#search-suggestions');if(!target)return;
    const q=state.query.trim().toLocaleLowerCase();
    const suggestions=[...new Set([...C.searches,...C.heroes.map(v=>v.short||v.title),...C.videos.map(v=>v.creator)])].filter(label=>label.toLocaleLowerCase().includes(q)&&label.toLocaleLowerCase()!==q).slice(0,3);
    target.innerHTML=q&&suggestions.length?`<small>${e(X.suggestions)}</small>${suggestions.map(label=>button(label,`query:${label}`,'search-suggestion','search')).join('')}`:'';
    const recent=document.querySelector('.recent-searches>div');if(recent)recent.innerHTML=(state.searchesCleared?[]:state.recentSearches||C.searches.slice(0,3)).map(label=>button(label,`query:${label}`,'chip')).join('');
  }
  function rememberQuery(){const q=state.query.trim();if(!q)return;state.searchesCleared=false;state.recentSearches=[q,...(state.recentSearches||C.searches.slice(0,3)).filter(item=>item!==q)].slice(0,3);updateSuggestions();}
  function filterSearch(items){return items.filter(v=>{if(v.live)return model.searchDuration==='all';const parts=v.duration.split(':').map(Number);const duration=parts[0]*60+parts[1];return model.searchDuration==='all'||model.searchDuration==='short'&&duration<600||model.searchDuration==='medium'&&duration>=600&&duration<=1800||model.searchDuration==='long'&&duration>1800;});}
  function handle(action,element){
    const [verb,key,index]=action.split(':');
    if(verb==='all'){model.listing=shelfRegistry.get(key);model.parent=state.page;navigate('collection');return true;}
    if(verb==='explore'){model.parent='series';navigate(key);return true;}
    if(verb==='explore-back'){navigate(model.parent);return true;}
    if(verb==='rank-genre'){model.rankGenre=key;renderPage(action);return true;}
    if(verb==='index-filter'){filterMenu(key);return true;}
    if(verb==='index-choice'){model.filters[key]=model.filterChoices[Number(index)];dismissModal();renderPage('index-filter:'+key);return true;}
    if(verb==='index-reset'){Object.keys(model.filters).forEach(key=>model.filters[key]=key==='sort'?X.sorts[0]:C.ui.any);renderPage(action);return true;}
    if(verb==='calendar-day'){model.day=Number(key);renderPage(action);return true;}
    if(verb==='sort'){searchMenu();return true;}
    if(verb==='search-sort'){state.sort=Number(key);document.querySelectorAll('.search-sort-options .chip').forEach(el=>el.classList.toggle('selected',el===element));return true;}
    if(verb==='search-duration'){model.searchDuration=key;document.querySelectorAll('.search-duration-options .chip').forEach(el=>el.classList.toggle('selected',el===element));return true;}
    if(verb==='search-apply'){dismissModal();document.querySelector('[data-action="sort"]').textContent=[C.ui.filters,C.ui.sortNew,C.ui.sortViews][state.sort];updateSearch();return true;}
    if(verb==='query'){state.query=decodeURIComponent(action.slice(6));document.querySelector('#search-input').value=state.query;rememberQuery();updateSearch();return true;}
    if(verb==='search-submit'){rememberQuery();document.querySelector('#search-results .focusable')?.focus();return true;}
    return false;
  }
  return {model,registerShelf,collection,ranking,index,calendar,navTools,homeTabs,handle,updateSuggestions,rememberQuery,filterSearch,back(){if(['collection','index','calendar'].includes(state.page)){navigate(model.parent);return true;}return false;}};
};
