'use strict';

window.createThemePages = ({state,icon,escape:e,button,modal,dismissModal,packages,preferenceRow,browseThemes}) => {
  const builtin={id:'mikutv-dark',name:'MikuTV Dark',builtin:true,palette:'mikutv-dark'};
  const palettes={
    'mikutv-dark':{canvas:'#080b11',sidebar:'#07090e',panel:'#111723',raised:'#192332',text:'#f4f5f8',secondary:'#a7b2c1',muted:'#9299a8',accent:'#39c5bb',soft:'#96e2d9',strong:'#299e95',ink:'#102d2b',focus:'#f4f5f8',focusText:'#111722',selected:'#123b37',selectedText:'#b2eee7',selectedFocus:'#166c63',ambient:'#13365020'},
    twilight:{canvas:'#10101c',sidebar:'#0c0c16',panel:'#1a1829',raised:'#242036',text:'#f5f0fc',secondary:'#bdb3cd',muted:'#a59bb8',accent:'#bda0ec',soft:'#dac6f7',strong:'#9f7ccc',ink:'#221332',focus:'#f5f0fc',focusText:'#21172e',selected:'#332442',selectedText:'#dac6f7',selectedFocus:'#654682',ambient:'#8b60c920'},
    ocean:{canvas:'#08151c',sidebar:'#071017',panel:'#102630',raised:'#18323d',text:'#eefaff',secondary:'#b1cbd5',muted:'#91b4c2',accent:'#60bfec',soft:'#b2e5ff',strong:'#4aa4d2',ink:'#072537',focus:'#eefaff',focusText:'#112935',selected:'#153d53',selectedText:'#b2e5ff',selectedFocus:'#245c7a',ambient:'#60bfec20'}
  };
  const model={current:builtin.id,failNext:false};
  const installed=()=>[builtin,...packages().filter(item=>item.kind==='theme'&&item.installed)];
  const current=()=>installed().find(item=>item.id===model.current)||builtin;
  const rgb=hex=>[1,3,5].map(offset=>parseInt(hex.slice(offset,offset+2),16)).join(' ');
  const focus=action=>requestAnimationFrame(()=>[...document.querySelectorAll('[data-action]')].find(node=>node.dataset.action===action)?.focus({preventScroll:true}));

  function apply(id) {
    const theme=installed().find(item=>item.id===id),colors=theme&&palettes[theme.palette];
    if(!colors||(model.failNext&&id!==builtin.id)){
      model.failNext=false;
      modal('\u65e0\u6cd5\u5e94\u7528\u4e3b\u9898',`<div data-theme-error><p>\u4e3b\u9898\u6682\u65f6\u65e0\u6cd5\u4f7f\u7528\uff0c\u5df2\u4fdd\u7559\u5f53\u524d\u5916\u89c2\u3002</p></div>`,button('\u91cd\u65b0\u9009\u62e9','theme-open','primary')+button('\u8fd4\u56de','dismissmodal','secondary'));
      return false;
    }
    const stage=document.querySelector('#stage');
    const variables={'--canvas':colors.canvas,'--canvas-rgb':rgb(colors.canvas),'--ink':colors.text,'--muted':colors.muted,'--accent':colors.accent,'--accent-rgb':rgb(colors.accent),'--accent-soft':colors.soft,'--accent-strong':colors.strong,'--accent-ink':colors.ink,'--accent-surface':colors.selected};
    for(const [key,value] of Object.entries(colors))variables['--theme-'+key.replace(/[A-Z]/g,char=>'-'+char.toLowerCase())]=value;
    for(const [key,value] of Object.entries(variables))stage.style.setProperty(key,value);
    stage.dataset.theme=id;stage.toggleAttribute('data-custom-theme',id!==builtin.id);model.current=id;
    const value=document.querySelector('[data-action="theme-open"] .preference-value');
    if(value)value.textContent=theme.name;
    return true;
  }
  function restore() {
    if(!installed().some(item=>item.id===model.current&&palettes[item.palette]))model.current=builtin.id;
    apply(model.current);
  }
  function renderRow() {
    return preferenceRow({label:'\u4e3b\u9898',note:'\u9009\u62e9\u5df2\u5b89\u88c5\u7684\u4e3b\u9898\uff0c\u66f4\u6539\u5e94\u7528\u914d\u8272\u3002',glyph:'palette',value:current().name,action:'theme-open'});
  }
  function swatch(item) {
    const colors=palettes[item.palette]||palettes[builtin.palette];
    return `<span class="theme-palette" aria-hidden="true">${['canvas','panel','selected','accent','soft','text'].map(key=>`<i style="background:${colors[key]}"></i>`).join('')}</span>`;
  }
  function openPicker(focusId=model.current) {
    if(state.overlay==='modal')dismissModal();
    const themes=installed();
    const options=themes.map(item=>`<button class="focusable preference-option theme-option ${item.id===model.current?'selected':''}" data-action="theme-select:${item.id}" role="radio" aria-checked="${item.id===model.current}">${swatch(item)}<span class="theme-copy"><strong>${e(item.name)}</strong><small>${item.builtin?'\u5185\u7f6e\u4e3b\u9898':'\u5df2\u5b89\u88c5\u7684\u4e3b\u9898\u63d2\u4ef6'}</small></span><span class="theme-check">${item.id===model.current?icon('check'):''}</span></button>`).join('');
    modal('\u9009\u62e9\u4e3b\u9898',`<div data-theme-picker><p>\u5e94\u7528\u4e8e\u6240\u6709\u5185\u5bb9\u5165\u53e3\u3002</p><div class="preference-options theme-options" role="radiogroup" aria-label="Theme">${options}</div>${themes.length===1?'<p class="theme-empty-note">\u5c1a\u672a\u5b89\u88c5\u5176\u4ed6\u4e3b\u9898\uff0c\u53ef\u4ece\u63d2\u4ef6\u76ee\u5f55\u6dfb\u52a0\u3002</p>':''}</div>`,button('\u53d1\u73b0\u66f4\u591a\u4e3b\u9898','theme-browse','secondary','search'));
    focus('theme-select:'+focusId);
  }
  function handle(action) {
    if(action==='theme-open'){openPicker();return true;}
    if(action?.startsWith('theme-select:')){dismissModal();apply(action.split(':')[1]);return true;}
    if(action==='theme-browse'){dismissModal();browseThemes();return true;}
    return false;
  }
  return {model,current,installed,apply,restore,renderRow,swatch,openPicker,handle};
};
