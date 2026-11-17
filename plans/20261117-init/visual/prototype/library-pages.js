'use strict';
window.createLibraryPages = ({content:C,state,icon,escape:e,button,header,card,find,modal,dismissModal,renderPage,startPlayer,login,toast}) => {
  const L=C.libraryPages;
  const model={folder:null,editing:false,selected:new Set(),chooser:null,choice:new Set(),editor:null,private:true,fromChooser:false};
  const folders=[
    {id:'default',title:L.defaultFolder,private:true,ids:new Set(['orbit','city'])},
    {id:'travel',title:L.travelFolder,private:false,ids:new Set(['mountains','ocean'])},
    {id:'weekend',title:L.weekendFolder,private:true,ids:new Set()}
  ];
  const currentFolder=()=>folders.find(item=>item.id===model.folder);
  const syncFavorites=()=>{state.favorites=new Set(folders.flatMap(folder=>[...folder.ids]));};
  const tabs=()=>[['history',C.ui.history],['watchlater',C.ui.watchlater],['favorites',C.ui.favorites],['tracking',C.ui.tracking],['courses',L.courses]];
  function ids(){
    if(state.libraryTab==='favorites')return [...(currentFolder()?.ids||[])];
    if(state.libraryTab==='history')return state.history;
    if(state.libraryTab==='watchlater')return [...state.later];
    if(state.libraryTab==='courses')return C.courses.map(item=>item.id);
    return [...state.tracked];
  }
  function folderCard(folder){
    const first=[...folder.ids][0];
    return `<button class="folder-card focusable" data-action="lib-open:${folder.id}"><div class="folder-cover">${first?`<img src="assets/${find(first).image}" alt="">`:`<span>${icon('folder')}</span>`}<i>${folder.ids.size}</i></div><strong>${e(folder.title)}</strong><small>${icon(folder.private?'lock':'user')}${e(folder.private?L.private:L.public)}</small></button>`;
  }
  function selectCard(id){
    const checked=model.selected.has(id);
    return `<div class="managed-card ${checked?'is-selected':''}">${card(find(id),{progress:state.libraryTab==='history'}).replace(`data-action="${find(id).live?'play':'detail'}:${id}"`,`data-action="lib-select:${id}" aria-pressed="${checked}"`)}<span class="selection-mark">${checked?icon('check'):''}</span></div>`;
  }
  function render(){
    const folder=currentFolder();
    const overview=state.libraryTab==='favorites'&&!folder;
    const list=ids();
    const title=folder?.title||C.ui.libraryTitle;
    const note=folder?`${folder.ids.size} ${L.items} · ${folder.private?L.private:L.public}`:C.ui.librarySub;
    const action=overview?button(L.create,'lib-create','secondary','plus'):list.length?button(model.editing?L.done:L.manage,'lib-manage','secondary',model.editing?'check':'settings'):'';
    let body='';
    if(!state.account)return `${header(tabs(),state.libraryTab,'library')}<section class="collection-empty"><span>${icon('library')}</span><h1>${e(L.guestTitle)}</h1><p>${e(L.guestBody)}</p>${button(C.ui.loginButton,'side-login','primary','user')}</section>`;
    if(overview)body=`<div class="folder-grid">${folders.map(folderCard).join('')}</div>`;
    else if(!list.length)body=`<section class="collection-empty"><span>${icon(state.libraryTab==='tracking'?'series':'library')}</span><h2>${e(L.emptyTitles[state.libraryTab])}</h2><p>${e(L.emptyNotes[state.libraryTab])}</p>${button(L.explore,'nav:home','secondary')}</section>`;
    else body=`${model.editing?`<div class="management-bar"><span>${e(L.selected)} ${model.selected.size}</span>${button(L.selectAll,'lib-all','chip')}${button(L.remove,'lib-remove','chip','trash',model.selected.size?'':'disabled')}</div>`:`<div class="collection-meta"><span>${e(folder?L.folderContents:tabs().find(([id])=>id===state.libraryTab)?.[1])}</span><small>${list.length} ${e(L.items)}</small>${state.libraryTab==='watchlater'?button(L.playAll,'lib-play-all','text-back','play'):''}</div>`}<div class="page-grid collection-grid">${list.map(id=>model.editing?selectCard(id):card(find(id),{progress:state.libraryTab==='history'})).join('')}</div>`;
    return `${header(tabs(),state.libraryTab,'library')}<div class="collection-heading">${folder?button(L.folders,'lib-back','text-back','back'):''}<div><h1>${e(title)}</h1><p>${e(note)}</p></div><div class="collection-actions">${action}${folder?button(L.folderSettings,'lib-settings','secondary','folder'):''}</div></div>${body}`;
  }
  function refresh(action=''){renderPage(action);}
  function chooser(id){
    model.chooser=id;model.choice=new Set(folders.filter(folder=>folder.ids.has(id)).map(folder=>folder.id));
    renderChooser();
  }
  function renderChooser(){
    modal(L.saveTitle,`<p class="dialog-context">${e(find(model.chooser).short||find(model.chooser).title)}</p><div class="folder-choices">${folders.map(folder=>`<button class="focusable folder-choice ${model.choice.has(folder.id)?'chosen':''}" data-action="lib-choice:${folder.id}" role="checkbox" aria-checked="${model.choice.has(folder.id)}">${icon('folder')}<span><strong>${e(folder.title)}</strong><small>${folder.ids.size} ${e(L.items)} · ${e(folder.private?L.private:L.public)}</small></span><i>${model.choice.has(folder.id)?icon('check'):''}</i></button>`).join('')}</div>${button(L.create,'lib-create-choice','text-back','plus')}`,button(L.save,'lib-save','primary')+button(C.ui.cancel,'dismissmodal','secondary'));
  }
  function editor(id=null,fromChooser=false){
    model.editor=id;model.fromChooser=fromChooser;const folder=folders.find(item=>item.id===id);model.private=folder?.private??true;
    modal(folder?L.editTitle:L.create,`<label class="field-label" for="folder-name">${e(L.folderName)}</label><input id="folder-name" class="focusable form-input" maxlength="24" value="${e(folder?.title||'')}" placeholder="${e(L.namePlaceholder)}"><button class="focusable folder-privacy" data-action="lib-privacy" role="switch" aria-checked="${model.private}"><span>${icon('lock')}${e(L.private)}</span><i class="switch ${model.private?'on':''}"></i></button><p class="form-error" id="folder-error" role="alert"></p>`,button(L.save,'lib-editor-save','primary')+button(C.ui.cancel,'lib-editor-cancel','secondary'));
    requestAnimationFrame(()=>document.querySelector('#folder-name')?.focus());
  }
  function handle(action,element){
    const [verb,key]=action.split(':');
    if(verb==='favorite'){chooser(state.player||state.detail);return true;}
    if(verb==='lib-open'){model.folder=key;model.editing=false;refresh('lib-back');return true;}
    if(verb==='lib-back'){model.folder=null;model.editing=false;refresh();return true;}
    if(verb==='library'){model.folder=null;model.editing=false;model.selected.clear();return false;}
    if(verb==='lib-manage'){model.editing=!model.editing;model.selected.clear();refresh(action);return true;}
    if(verb==='lib-select'){model.selected.has(key)?model.selected.delete(key):model.selected.add(key);refresh(action);return true;}
    if(verb==='lib-all'){const all=ids();model.selected=model.selected.size===all.length?new Set():new Set(all);refresh(action);return true;}
    if(verb==='lib-remove'){
      const selected=new Set(model.selected);
      if(state.libraryTab==='history')state.history=state.history.filter(id=>!selected.has(id));
      if(state.libraryTab==='watchlater')selected.forEach(id=>state.later.delete(id));
      if(state.libraryTab==='tracking')selected.forEach(id=>state.tracked.delete(id));
      if(state.libraryTab==='favorites')selected.forEach(id=>currentFolder().ids.delete(id));
      model.selected.clear();syncFavorites();refresh('lib-manage');toast(L.removed);return true;
    }
    if(verb==='lib-play-all'){startPlayer(ids()[0]);return true;}
    if(verb==='lib-create'){editor();return true;}
    if(verb==='lib-create-choice'){dismissModal();editor(null,true);return true;}
    if(verb==='lib-settings'){
      modal(currentFolder().title,`<div class="preference-options">${button(L.editTitle,'lib-edit','preference-option','settings')}${currentFolder().id==='default'?'':button(L.deleteFolder,'lib-delete','preference-option','trash')}</div>`,button(C.ui.close,'dismissmodal','secondary'));return true;
    }
    if(verb==='lib-edit'){dismissModal();editor(model.folder);return true;}
    if(verb==='lib-delete'){dismissModal();modal(L.deleteTitle,`<p>${e(L.deleteBody)}</p>`,button(L.deleteFolder,'lib-delete-confirm','primary','trash')+button(C.ui.cancel,'dismissmodal','secondary'));return true;}
    if(verb==='lib-delete-confirm'){folders.splice(folders.findIndex(folder=>folder.id===model.folder),1);syncFavorites();model.folder=null;dismissModal();refresh();return true;}
    if(verb==='lib-privacy'){model.private=!model.private;element.setAttribute('aria-checked',model.private);element.querySelector('.switch').classList.toggle('on',model.private);return true;}
    if(verb==='lib-editor-cancel'){dismissModal();if(model.fromChooser)renderChooser();return true;}
    if(verb==='lib-editor-save'){
      const title=document.querySelector('#folder-name').value.trim();
      const error=!title?L.required:folders.some(folder=>folder.id!==model.editor&&folder.title.toLocaleLowerCase()===title.toLocaleLowerCase())?L.duplicate:'';
      if(error){document.querySelector('#folder-error').textContent=error;document.querySelector('#folder-name').focus();return true;}
      if(model.editor){const folder=folders.find(item=>item.id===model.editor);folder.title=title;folder.private=model.private;}
      else{const folder={id:'folder-'+Date.now(),title,private:model.private,ids:new Set()};folders.push(folder);if(model.fromChooser)model.choice.add(folder.id);}
      dismissModal();if(model.fromChooser)renderChooser();else refresh('lib-create');return true;
    }
    if(verb==='lib-choice'){model.choice.has(key)?model.choice.delete(key):model.choice.add(key);element.setAttribute('aria-checked',model.choice.has(key));element.classList.toggle('chosen',model.choice.has(key));element.querySelector('i').innerHTML=model.choice.has(key)?icon('check'):'';return true;}
    if(verb==='lib-save'){folders.forEach(folder=>model.choice.has(folder.id)?folder.ids.add(model.chooser):folder.ids.delete(model.chooser));syncFavorites();dismissModal();toast(L.saved);if(state.page==='library'&&!state.overlay)refresh();return true;}
    return false;
  }
  C.courses.forEach(course=>state.owned.add(course.id));
  syncFavorites();
  return {render,handle,folders,model,chooser,saveToDefault(id){folders[0].ids.add(id);syncFavorites();},back(){if(state.page!=='library')return false;if(model.editing){model.editing=false;refresh('lib-manage');return true;}if(model.folder){model.folder=null;refresh();return true;}return false;}};
};
