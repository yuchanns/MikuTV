'use strict';
window.createTextInput = ({content:C,state,icon,escape:e,button}) => {
  const T=C.textInput;
  const model={field:null,returnFocus:null,mode:'abc',wasPlaying:false,title:'',draft:'',onCommit:null};
  const layer=()=>document.querySelector('#system-input-layer');
  function render(){
    const chars=model.mode==='numbers'?'0123456789.,?!@#&()-:':model.mode==='ABC'?'ABCDEFGHIJKLMNOPQRSTUVWXYZ':'abcdefghijklmnopqrstuvwxyz';
    const markup=`<section class="system-keyboard" role="dialog" aria-modal="true" aria-label="${e(model.title)}"><h2>${e(model.title)}</h2><input class="focusable system-text" id="system-text" type="text" maxlength="${model.field.maxLength>0?model.field.maxLength:500}" value="${e(model.draft)}" placeholder="${e(model.field.placeholder||T.placeholder)}" aria-label="${e(model.title)}"><div class="system-keyboard-modes">${[['abc','abc'],['ABC','ABC'],['numbers','123']].map(([key,label])=>button(label,`input-mode:${key}`,`system-mode ${model.mode===key?'selected':''}`)).join('')}</div><div class="system-keyboard-line">${chars.split('').map(char=>button(char,`input-char:${encodeURIComponent(char)}`,'system-key')).join('')}${button('','input-delete','system-key system-delete','back',`aria-label="${e(T.delete)}"`)}</div><div class="system-keyboard-actions">${button(T.space,'input-space','system-input-action')}${button(T.clear,'input-clear','system-input-action')}${button(T.done,'input-done','primary')}</div></section>`;
    if(!layer())document.querySelector('#stage').insertAdjacentHTML('beforeend','<div id="system-input-layer"></div>');
    layer().innerHTML=markup;
    requestAnimationFrame(()=>document.querySelector('#system-text')?.focus({preventScroll:true}));
  }
  function openField(field,{title,onCommit,returnFocus}={}){
    if(layer())return;
    model.field=field;model.returnFocus=returnFocus||document.activeElement;model.mode=field.inputMode==='numeric'?'numbers':'abc';model.draft=field.value||'';model.onCommit=onCommit;
    model.title=title||field.closest('.modal')?.querySelector('h2')?.textContent||field.getAttribute('aria-label')||T.title;
    model.wasPlaying=!!(state.player&&state.playing);if(model.wasPlaying)state.playing=false;
    render();
  }
  function close(commit){
    const input=document.querySelector('#system-text');if(!input)return;
    if(commit&&model.field?.isConnected){model.field.value=input.value.slice(0,model.field.maxLength>0?model.field.maxLength:500);model.field.dispatchEvent(new Event('input',{bubbles:true}));model.onCommit?.(model.field.value);}
    layer().remove();if(model.wasPlaying&&!state.trialEnded)state.playing=true;
    if(model.returnFocus?.isConnected)model.returnFocus.focus({preventScroll:true});
  }
  function edit(transform){
    const input=document.querySelector('#system-text');const next=transform(input.value);input.value=next.slice(0,input.maxLength);model.draft=input.value;
  }
  function handle(action){
    const [verb,key]=action.split(':');
    if(!verb.startsWith('input-'))return false;
    if(verb==='input-char')edit(text=>text+decodeURIComponent(key));
    if(verb==='input-space')edit(text=>text+' ');
    if(verb==='input-delete')edit(text=>[...text].slice(0,-1).join(''));
    if(verb==='input-clear')edit(()=>'');
    if(verb==='input-mode'){model.draft=document.querySelector('#system-text').value;model.mode=key;render();}
    if(verb==='input-done')close(true);
    return true;
  }
  document.addEventListener('click',event=>{
    const field=event.target.closest('#stage input:not([type="range"]):not([type="hidden"]),#stage textarea');
    if(!field||field.id==='system-text'||layer())return;
    event.preventDefault();event.stopImmediatePropagation();openField(field);
  },true);
  return {openField,close,handle,model,active:()=>!!layer(),handleKey(event){
    if(!layer())return false;
    if(event.key==='Escape'){event.preventDefault();close(false);return true;}
    if(event.key==='Enter'&&event.target?.id==='system-text'){event.preventDefault();close(true);return true;}
    return false;
  }};
};
