(() => {
  'use strict';
  const pad=value=>String(value).padStart(2,'0');
  function period(date=new Date()){
    const hour=date.getHours();
    if(hour>=5&&hour<11)return 'morning';
    if(hour>=11&&hour<14)return 'midday';
    if(hour>=14&&hour<18)return 'afternoon';
    if(hour>=18&&hour<23)return 'evening';
    return 'night';
  }
  const copy=(date=new Date())=>window.CONTENT.editorial[period(date)];
  const feature=(index=0,date=new Date())=>`${copy(date).feature} / ${pad(index+1)}`;
  const series=(date=new Date())=>copy(date).series;
  const clock=(date=new Date())=>`${pad(date.getHours())}:${pad(date.getMinutes())}`;
  function refresh(date=new Date()){
    document.querySelectorAll('[data-editorial="feature"]').forEach(element=>{element.textContent=feature(Number(element.dataset.featureIndex),date);});
    document.querySelectorAll('[data-editorial="series"]').forEach(element=>{element.textContent=series(date);});
    document.querySelectorAll('[data-local-clock]').forEach(element=>{element.textContent=clock(date);});
  }
  function schedule(){
    setTimeout(()=>{refresh();schedule();},60000-Date.now()%60000);
  }
  window.EDITORIAL_TIME={period,feature,series,clock,refresh};
  document.addEventListener('visibilitychange',()=>{if(!document.hidden)refresh();});
  window.addEventListener('focus',()=>refresh());
  refresh();schedule();
})();
