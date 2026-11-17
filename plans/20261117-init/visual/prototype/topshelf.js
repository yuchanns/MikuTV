(() => {
 'use strict';
 const C=window.CONTENT;const L=C.topShelf;let selected=0;let immersive=false;
 const entries=[C.heroes[0],C.heroes[1],C.shows.find(v=>v.id==='show-4')];
 const neighbors=[['netflix','Netflix'],['youtube','YouTube'],['tv','Apple TV'],['music','Music'],['plex','Plex']];
 const $=s=>document.querySelector(s);
 const e=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 const icon=name=>window.LUCIDE_ICONS[name]||window.LUCIDE_ICONS.play;
 function render(){
  const v=entries[selected];
  $('#top-shelf').classList.toggle('top-immersive',immersive);
  $('#top-shelf').innerHTML=`<img class="top-art" src="assets/${e(v.image)}" alt=""><div class="top-shade"></div><div class="top-status"><span data-local-clock>${window.EDITORIAL_TIME.clock()}</span><div class="profile-dot">H</div></div><div class="top-copy"><div class="top-context"><img src="assets/app-icon/app-icon.svg" alt=""><span>${e(selected===0?L.continue:L.featured)}</span></div><h1>${e(v.title)}</h1><div class="meta"><span class="badge">${e(v.tag)}</span><span>${e(v.category)}</span><i class="dot"></i><span>${e(v.duration)}</span></div><p class="top-summary">${e(v.description||C.ui.seriesSub).replace(/\n/g,'<br>')}</p><div class="top-actions"><button class="top-action primary-action focusable" data-top="play">${icon('play')}${e(selected===0?L.play:C.ui.play)}</button><button class="top-action focusable" data-top="info">${icon('info')}${e(L.info)}</button></div></div><div class="top-mini-row">${entries.map((item,i)=>`<button class="top-mini focusable ${i===selected?'selected':''}" data-top="pick:${i}" aria-label="${e(item.short||item.title)}"><img src="assets/${e(item.image)}" alt=""></button>`).join('')}</div><div class="top-switch"><button class="focusable" data-top="previous" aria-label="Previous">${icon('back')}</button><span>${selected+1} / ${entries.length}</span><button class="focusable" data-top="next" aria-label="Next">${icon('arrow')}</button></div><div class="tv-dock"><button class="tv-app current focusable" data-top="app" aria-label="${e(L.app)}"><div class="app-icon-layers"><img class="icon-back" src="assets/app-icon/background.svg" alt=""><img class="icon-mid" src="assets/app-icon/midground.svg" alt=""><img class="icon-front" src="assets/app-icon/foreground.svg" alt=""></div><span>${e(L.app)}</span></button>${neighbors.map(([name,label])=>`<div class="tv-app neighbor app-${name}" aria-hidden="true"><img src="assets/tv-apps/${name}.png" alt=""><span>${e(label)}</span></div>`).join('')}</div>`;
 }
 function act(action){
  const v=entries[selected];
  if(action==='play'||action==='info'){location.href=`index.html?route=${action==='play'?'play':'detail'}&id=${encodeURIComponent(v.id)}`;return;}
  if(action==='app'){location.href='index.html';return;}
  if(action==='other')return;
  if(action==='next')selected=(selected+1)%entries.length;
  if(action==='previous')selected=(selected+entries.length-1)%entries.length;
  if(action.startsWith('pick:'))selected=Number(action.split(':')[1]);
  render();
 }
 function resize(){const viewport=$('.viewport');$('#stage').style.transform=`scale(${Math.min(viewport.clientWidth/1920,viewport.clientHeight/1080)})`;}
 document.addEventListener('click',event=>{const target=event.target.closest('[data-top]');if(target)act(target.dataset.top);});
 document.addEventListener('pointermove',event=>{const tile=event.target.closest('.tv-app.current');if(!tile)return;const r=tile.getBoundingClientRect();const x=(event.clientX-r.left)/r.width-.5;const y=(event.clientY-r.top)/r.height-.5;const front=tile.querySelector('.icon-front');if(front)front.style.transform=`translate(${x*7}px,${y*5-2}px)`;});
 document.addEventListener('keydown',event=>{
  if(event.key==='ArrowLeft'||event.key==='ArrowRight'){event.preventDefault();act(event.key==='ArrowRight'?'next':'previous');}
  if(event.key==='ArrowUp'){event.preventDefault();immersive=true;render();$('.top-action')?.focus();}
  if(event.key==='ArrowDown'||event.key==='Escape'){event.preventDefault();immersive=false;render();$('.tv-app.current')?.focus();}
 });
 $('#immersive-button').addEventListener('click',()=>{immersive=!immersive;render();});
 $('#top-review-label').textContent=L.previewTitle;$('#top-help').textContent=L.help;
 window.addEventListener('resize',resize);render();resize();
})();
