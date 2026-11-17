'use strict';
window.createCommunityPages = ({content:C,state,icon,escape:e,button,header,card,find,allVideos,modal,dismissModal,navigate,renderPage,textInput,toast}) => {
  const P=C.community;
  const model={tab:'notifications',filter:'all',contact:'orbit',creator:null,creatorTab:'videos',originCreator:null,draft:''};
  const notifications=P.notifications.map(item=>({...item}));
  const contacts=P.contacts.map(item=>({...item,messages:item.messages.map(message=>({...message}))}));
  const unread=()=>notifications.filter(item=>!item.read).length+contacts.reduce((count,item)=>count+item.unread,0);
  const title=()=>`<div class="collection-heading"><div><h1>${e(P.title)}</h1><p>${e(P.subtitle)}</p></div><div class="collection-actions">${button(P.markRead,'community-read-all','secondary','check')}</div></div>`;
  function messages(){
    if(!state.account)return `${header()}<section class="collection-empty"><span>${icon('comment')}</span><h1>${e(P.guest)}</h1>${button(C.ui.loginButton,'side-login','primary','user')}</section>`;
    const tabs=[['notifications',P.notificationsTab],['private',P.privateTab]];
    let body='';
    if(model.tab==='notifications'){
      const items=notifications.filter(item=>model.filter==='all'||item.type===model.filter);
      body=`<div class="message-filters">${['all','reply','like','system'].map(key=>button(P.filters[key],`community-filter:${key}`,`chip ${model.filter===key?'selected':''}`)).join('')}</div><div class="notification-list">${items.map(item=>`<button class="focusable notification-row ${item.read?'':'unread'}" data-action="community-notice:${item.id}"><img class="notification-avatar" src="assets/${item.avatar}" alt=""><span class="notification-copy"><strong>${e(item.actor)}<em>${e(P.actions[item.type])}</em></strong><span>${e(item.text)}</span><small>${e(item.time)}</small></span>${item.video?`<img class="notification-cover" src="assets/${find(item.video).image}" alt="">`:icon('info')}${item.read?'':'<i class="unread-dot"></i>'}</button>`).join('')}</div>`;
    }else{
      const active=contacts.find(item=>item.id===model.contact)||contacts[0];
      body=`<div class="private-layout"><nav class="private-contacts">${contacts.map(item=>`<button class="focusable private-contact ${active.id===item.id?'selected':''}" data-action="community-contact:${item.id}"><img src="assets/${item.avatar}" alt=""><span><strong>${e(item.name)}</strong><small>${e(item.messages.at(-1)?.text||P.newConversation)}</small></span>${item.unread?`<i>${item.unread}</i>`:''}</button>`).join('')}</nav><section class="private-conversation"><header><img src="assets/${active.avatar}" alt=""><h2>${e(active.name)}</h2><span>${e(P.privateConversation)}</span></header><div class="private-scroll scroll-region focusable" tabindex="0">${active.messages.map(item=>`<div class="private-message ${item.own?'own':''}"><span>${e(item.text)}</span><small>${e(item.time)}</small></div>`).join('')}</div>${button(P.write,'community-compose','private-compose','comment')}</section></div>`;
    }
    return `${header(tabs,model.tab,'community-tab')}${title()}${body}`;
  }
  function renderCreator(){
    const v=find(model.creator);const videos=allVideos.filter(item=>!item.live&&item.creator===v.creator);
    const tabs=[['videos',P.videos],['posts',P.posts],['collections',P.collections],['about',P.about]];
    const followed=state.following.has(v.creator);
    let body='';
    if(model.creatorTab==='videos')body=`<div class="page-grid creator-video-grid">${videos.map(item=>card(item)).join('')}</div>`;
    if(model.creatorTab==='posts')body=`<div class="creator-posts">${videos.slice(0,3).map(item=>`<article class="creator-post"><div><img src="assets/${v.image}" alt=""><span><strong>${e(v.creator)}</strong><small>${e(P.postTime)}</small></span></div><p>${e(P.postText)}</p>${card(item)}</article>`).join('')}</div>`;
    if(model.creatorTab==='collections')body=`<div class="creator-collection"><h2>${e(P.collectionTitle)}</h2><p>${videos.length} ${e(P.works)}</p><div class="cards">${videos.map(item=>card(item)).join('')}</div></div>`;
    if(model.creatorTab==='about')body=`<div class="creator-about"><h2>${e(v.creator)}</h2><p>${e(P.bio)}</p><div><span>${e(P.location)}</span><strong>${e(P.locationValue)}</strong></div><div><span>${e(P.category)}</span><strong>${e(v.category)}</strong></div><div><span>${e(P.works)}</span><strong>${videos.length}</strong></div></div>`;
    document.querySelector('#overlay').innerHTML=`<article class="detail creator-expanded"><div class="creator-cover" style="background-image:url('assets/${v.image}')"></div><div class="creator-overview"><img class="creator-large-avatar" src="assets/${v.image}" alt=""><div class="creator-heading"><span>CREATOR SPOTLIGHT</span><h1>${e(v.creator)}</h1><p>${e(P.bio)}</p><div class="creator-metrics"><span><b>12.8${e(P.tenThousand)}</b>${e(P.followers)}</span><span><b>${videos.length}</b>${e(P.works)}</span></div></div><div class="creator-profile-actions">${button(followed?C.ui.followed:C.ui.follow,`community-follow:${v.id}`,'primary',followed?'check':'plus')}${button(P.privateTab,`community-private:${v.id}`,'secondary','comment')}</div></div><div class="creator-tabs tabs">${tabs.map(([id,label])=>button(label,`community-creator-tab:${id}`,`tab ${model.creatorTab===id?'selected':''}`)).join('')}</div><section class="creator-content">${body}</section></article>`;
  }
  function openCreator(id,restore=null){
    state.returnFocus=restore?.focus||document.activeElement?.dataset.action;state.overlay='creator';state.detail=null;state.player=null;
    model.creator=id;model.creatorTab=restore?.tab||'videos';renderCreator();
    requestAnimationFrame(()=>document.querySelector('#overlay').querySelector(`[data-action="${restore?.focus||'community-follow:'+id}"]`)?.focus({preventScroll:true}));
  }
  function compose(){
    model.draft='';
    modal(P.write,`<p class="dialog-context">${e(contacts.find(item=>item.id===model.contact).name)}</p><input type="hidden" id="private-draft" maxlength="300" value=""><p class="private-draft-copy">${e(P.draftPlaceholder)}</p>`,button(P.edit,'community-edit','secondary')+button(P.send,'community-send','primary','send','disabled'));
    requestAnimationFrame(editDraft);
  }
  function editDraft(){textInput.openField(document.querySelector('#private-draft'),{title:P.write,returnFocus:document.querySelector('[data-action="community-edit"]'),onCommit(text){model.draft=text;document.querySelector('.private-draft-copy').textContent=text||P.draftPlaceholder;const send=document.querySelector('[data-action="community-send"]');send.disabled=!text.trim();requestAnimationFrame(()=>send.disabled?document.querySelector('[data-action="community-edit"]').focus():send.focus());}});}
  function handle(action,element){
    const [verb,key]=action.split(':');
    if(verb==='community-messages'){model.originCreator=null;navigate('messages',{parent:'account'});return true;}
    if(verb==='community-tab'){model.tab=key;renderPage(action);return true;}
    if(verb==='community-filter'){model.filter=key;renderPage(action);return true;}
    if(verb==='community-read-all'){notifications.forEach(item=>item.read=true);contacts.forEach(item=>item.unread=0);renderPage(action);return true;}
    if(verb==='community-notice'){const item=notifications.find(item=>item.id===key);item.read=true;renderPage(action);if(item.video){window.MikuPrototype.openDetail(item.video);if(item.type==='reply')window.MikuPrototype.perform('detailtab:comments');}else modal(item.actor,`<p>${e(item.text)}</p>`);return true;}
    if(verb==='community-contact'){model.contact=key;contacts.find(item=>item.id===key).unread=0;renderPage(action);return true;}
    if(verb==='community-creator-tab'){model.creatorTab=key;renderCreator();document.querySelector(`[data-action="${action}"]`)?.focus({preventScroll:true});return true;}
    if(verb==='community-follow'){const v=find(key);state.following.has(v.creator)?state.following.delete(v.creator):state.following.add(v.creator);renderCreator();document.querySelector(`[data-action="${action}"]`)?.focus({preventScroll:true});return true;}
    if(verb==='community-private'){
      const v=find(key);let contact=contacts.find(item=>item.id===key);if(!contact){contact={id:key,name:v.creator,avatar:v.image,unread:0,messages:[]};contacts.push(contact);}
      model.originCreator={id:model.creator,tab:model.creatorTab,focus:action,parent:state.page};model.contact=key;model.tab='private';navigate('messages',{parent:state.page});return true;
    }
    if(verb==='community-compose'){compose();return true;}
    if(verb==='community-edit'){editDraft();return true;}
    if(verb==='community-send'){const text=document.querySelector('#private-draft').value.trim();if(!text)return true;contacts.find(item=>item.id===model.contact).messages.push({text,own:true,time:P.now});dismissModal();renderPage('community-compose');toast(P.sent);return true;}
    return false;
  }
  return {messages,handle,openCreator,renderCreator,model,unread,back(){if(state.page!=='messages'||!model.originCreator)return false;const origin=model.originCreator;model.originCreator=null;navigate(origin.parent);openCreator(origin.id,origin);return true;}};
};
