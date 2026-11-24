'use strict';
window.createSidebarPages = ({content:C,state,icon,escape:e,button,header,modal,dismissModal,navigate,renderPage,login,rightsNotice,toast,unreadMessages,renderPlugins,renderThemeRow,switchAccount,accountSignedOut}) => {
  const S=C.sidebarPages.settings;
  const A=C.sidebarPages.account;
  const B=S.rules;
  const rules=B.seed.map((text,index)=>({id:index+1,text,type:index===2?'user':'keyword',enabled:true}));
  const ruleModel={open:false,filter:'all',type:'keyword',draft:''};
  const model={section:'playback',quality:S.auto,speed:'1.0x',autoplay:true,sync:true,size:S.normal,opacity:'75%',density:'60%',area:S.upper};
  const choiceValues={quality:[S.auto,'4K HDR','4K','1080P','720P'],speed:['0.75x','1.0x','1.25x','1.5x','2.0x'],subtitles:[C.ui.off,C.ui.chinese,C.ui.english],opacity:['50%','75%','100%'],size:[S.normal,S.large],density:['30%','60%','100%'],area:[S.upper,S.full,S.top]};
  const toggleKeys=new Set(['autoplay','danmaku','preview','motion','sync']);
  const title=(name,note)=>`<div class="page-title utility-title"><h1>${e(name)}</h1><p>${e(note)}</p></div>`;
  function value(key){
    if(key==='danmaku')return state.danmaku;
    if(key==='preview')return state.preview;
    if(key==='motion')return state.reduced;
    if(key==='subtitles')return state.subtitles;
    if(key==='opacity')return `${state.dmOpacity??75}%`;
    if(key==='density')return `${state.dmDensity??60}%`;
    if(key==='area')return state.dmArea==='full'?S.full:state.dmArea==='top'?S.top:S.upper;
    return model[key];
  }
  function preferenceRow({label,note='',glyph='info',value='',action='',toggle=null,visual=''}) {
    const tag=action?'button':'div';
    const attributes=toggle!==null?`role="switch" aria-checked="${toggle}"`:action?'aria-haspopup="dialog"':'';
    return `<${tag} class="preference-row ${action?'focusable':''}" ${action?`data-action="${e(action)}"`:''} ${attributes}><span class="preference-icon">${icon(glyph)}</span><span class="preference-copy"><strong>${e(label)}</strong>${note?`<small>${e(note)}</small>`:''}</span>${visual?`<span class="preference-preview">${visual}</span>`:''}${toggle!==null?`<i class="switch ${toggle?'on':''}"></i>`:value?`<span class="preference-value">${e(value)}</span>`:''}</${tag}>`;
  }
  function row(key){
    const [label,note,glyph]=S.rows[key];
    return preferenceRow({label,note,glyph,value:value(key)||'',action:'side-pref:'+key,toggle:toggleKeys.has(key)?value(key):null});
  }
  function settings(){
    const [heading,note]=S.headings[model.section]||[];
    const rows={playback:['quality','speed','subtitles','autoplay'],danmaku:['danmaku','density','area','opacity','size','blocklist'],appearance:['preview','motion'],privacy:['sync','clearsearch','clearhistory']};
    const about=`<div class="about-overview"><div class="about-app"><img src="assets/app-icon/app-icon.svg" alt=""><h2>${e(C.brand)}</h2><small>${e(note)}</small></div><div class="about-facts"><div><span>${e(S.about.version)}</span><strong>${e(S.about.versionValue)}</strong></div><div><span>${e(S.about.licenses)}</span><strong>${e(S.about.license)}</strong></div><div><span>${e(S.about.platform)}</span><strong>${e(S.about.platformValue)}</strong></div><button class="focusable" data-action="side-license"><span>${e(S.about.licenses)}</span></button><button class="focusable" data-action="side-data"><span>${e(S.about.privacy)}</span></button><a class="focusable" href="https://github.com/yuchanns/MikuTV" target="_blank" rel="noreferrer"><span>${e(S.about.project)}</span></a></div></div>`;
    const body=model.section==='plugins'?renderPlugins():model.section==='danmaku'&&ruleModel.open?renderRules():model.section==='about'?about:`<h2>${e(heading)}</h2><p class="preference-intro">${e(note)}</p><div class="preference-list ${model.section==='danmaku'?'danmaku-preferences':''}">${model.section==='appearance'?renderThemeRow():''}${rows[model.section].map(row).join('')}</div>`;
    const sections=[...S.sections];sections.splice(-1,0,['plugins','\u63d2\u4ef6\u7ba1\u7406','plug']);
    return `${header()}${title(S.title,S.subtitle)}<div class="settings-layout"><nav class="settings-sections" aria-label="${e(S.title)}">${sections.map(([id,label,glyph])=>`<button class="focusable ${model.section===id?'selected':''}" data-action="side-section:${id}" aria-current="${model.section===id?'page':'false'}">${icon(glyph)}<span>${e(label)}</span></button>`).join('')}</nav><section class="preferences">${body}</section></div>`;
  }
  function renderRules(){
    const visible=rules.filter(rule=>ruleModel.filter==='all'||rule.type===ruleModel.filter);
    return `<div class="rules-heading">${button(B.back,'side-rules-back','text-back','back')}<span>${e(state.account?B.synced:B.guest)}</span></div><div class="section-heading"><div><h2>${e(B.title)}</h2><p>${e(B.note)}</p></div>${button(B.add,'side-rule-add','secondary','plus')}</div><div class="rule-tabs">${['all','keyword','user'].map(type=>button(B[type],`side-rule-filter:${type}`,`tab ${ruleModel.filter===type?'selected':''}`)).join('')}</div><div class="rule-list">${visible.length?visible.map(rule=>`<div class="rule-item"><button class="focusable rule-toggle" data-action="side-rule-toggle:${rule.id}" role="switch" aria-checked="${rule.enabled}"><span class="rule-kind">${e(B[rule.type])}</span><strong>${e(rule.text)}</strong><i class="switch ${rule.enabled?'on':''}"></i></button><button class="focusable rule-remove round-close" data-action="side-rule-remove:${rule.id}" aria-label="${e(B.remove)}">${icon('trash')}</button></div>`).join(''):`<div class="rules-empty">${icon('lock')}<h3>${e(B.empty)}</h3><p>${e(B.emptyNote)}</p></div>`}</div>`;
  }
  function ruleEditor(){
    modal(B.addTitle,`<div class="rule-tabs">${['keyword','user'].map(type=>button(B[type],`side-rule-type:${type}`,`tab ${ruleModel.type===type?'selected':''}`)).join('')}</div><input id="rule-input" inputmode="${ruleModel.type==='user'?'numeric':'text'}" class="focusable form-input" maxlength="40" value="${e(ruleModel.draft)}" placeholder="${e(ruleModel.type==='user'?B.userPlaceholder:B.placeholder)}" aria-label="${e(B.title)}"><p class="form-error" id="rule-error" role="alert"></p>`,button(B.save,'side-rule-save','primary')+button(S.cancel,'dismissmodal','secondary'));
    requestAnimationFrame(()=>document.querySelector('#rule-input')?.focus());
  }
  function account(){
    if(!state.account)return `${header()}${title(A.title,A.subtitle)}<section class="account-guest"><div class="account-guest-art"><img src="assets/hero-orbit.png" alt=""></div><div class="account-guest-copy"><span class="guest-avatar">${icon('user')}</span><h2>${e(A.welcome)}</h2><p>${e(A.guestBody)}</p>${button(A.login,'side-login','primary','user')}</div></section>`;
    const counts={history:state.history.length,favorites:state.favorites.size,watchlater:state.later.size};
    return `${header()}${title(A.title,A.subtitle)}${button(C.community.title+(unreadMessages()?' · '+unreadMessages():''),'community-messages','account-notifications secondary','comment')}<div class="account-layout"><section class="account-profile"><div class="account-identity"><div class="account-avatar">${e(state.accountInitial||'H')}</div><div><span class="account-overline">${e(A.connected)}</span><h2>${e(state.accountName||C.nav.account)}</h2><span class="account-tier">${icon(state.member?'sparkles':'user')}${e(state.member?A.vip:A.normal)}</span></div></div>${button(A.switch,'side-login','account-switch','user')}<div class="account-shortcuts">${A.shortcuts.map(([id,label,glyph])=>`<button class="focusable" data-action="side-library:${id}">${icon(glyph)}<strong>${counts[id]}</strong><span>${e(label)}</span></button>`).join('')}</div></section><section class="membership-card"><div class="membership-heading">${icon('sparkles')}<span>${e(A.vip)}</span></div><h2>${e(A.memberTitle)}</h2><ul>${A.memberBenefits.map(text=>`<li>${icon('check')}${e(text)}</li>`).join('')}</ul>${button(state.member?A.memberActive:A.memberAction,'side-membership','membership-action')}</section></div><section class="account-sync"><div><span class="account-sync-icon">${icon('history')}</span><div><h3>${e(model.sync?A.syncOn:A.syncOff)}</h3><p>${e(A.syncBody)}</p></div></div>${button(A.syncAction,'side-sync','secondary','settings')}</section><div class="account-bottom"><span><i></i>${e(A.status)} · ${e(A.statusValue)}</span>${button(A.logout,'side-logout','account-logout','logout')}</div>`;
  }
  function refresh(action){renderPage(action);}
  function accountFocus(action,direction){
    if(!state.account)return '';
    const shortcuts=A.shortcuts.map(([id])=>'side-library:'+id);
    const index=shortcuts.indexOf(action);
    if(index>=0){
      if(direction==='ArrowUp')return 'side-login';
      if(direction==='ArrowDown')return 'side-sync';
      if(direction==='ArrowLeft')return shortcuts[index-1]||'account';
      return shortcuts[index+1]||'';
    }
    return {
      'community-messages':{ArrowDown:'side-login',ArrowLeft:'account'},
      'side-login':{ArrowUp:'community-messages',ArrowDown:shortcuts[0],ArrowRight:'side-membership',ArrowLeft:'account'},
      'side-membership':{ArrowUp:'community-messages',ArrowDown:shortcuts[0],ArrowLeft:'side-login'},
      'side-sync':{ArrowUp:shortcuts[0],ArrowDown:'side-logout',ArrowLeft:'account'},
      'side-logout':{ArrowUp:'side-sync',ArrowLeft:'account'},
      'account':{ArrowRight:'side-login'}
    }[action]?.[direction]||'';
  }
  function handle(action){
    const [verb,key]=action.split(':');
    if(verb==='side-rules-back'){ruleModel.open=false;refresh('side-pref:blocklist');return true;}
    if(verb==='side-rule-filter'){ruleModel.filter=key;refresh(action);return true;}
    if(verb==='side-rule-toggle'){const rule=rules.find(item=>item.id===Number(key));rule.enabled=!rule.enabled;refresh(action);return true;}
    if(verb==='side-rule-remove'){const index=rules.findIndex(item=>item.id===Number(key));rules.splice(index,1);refresh('side-rule-add');toast(B.deleted);return true;}
    if(verb==='side-rule-add'){ruleModel.type='keyword';ruleModel.draft='';ruleEditor();return true;}
    if(verb==='side-rule-type'){ruleModel.draft=document.querySelector('#rule-input').value;ruleModel.type=key;dismissModal();ruleEditor();return true;}
    if(verb==='side-rule-save'){
      const text=document.querySelector('#rule-input').value.trim();
      const error=!text?B.required:ruleModel.type==='user'&&!/^\d+$/.test(text)?B.invalidUser:rules.some(rule=>rule.type===ruleModel.type&&rule.text===text)?B.duplicate:'';
      if(error){document.querySelector('#rule-error').textContent=error;document.querySelector('#rule-input').focus();return true;}
      rules.push({id:Date.now(),text,type:ruleModel.type,enabled:true});dismissModal();refresh('side-rule-add');return true;
    }
    if(verb==='side-license'){modal(S.about.licenseTitle,`<pre class="license-copy scroll-region focusable" tabindex="0">${e(S.licenseText)}</pre>`);return true;}
    if(verb==='side-data'){modal(S.about.privacyTitle,`<p class="privacy-copy">${e(S.about.privacyBody)}</p>`,button(S.about.privacyAction,'side-data-open','primary'));return true;}
    if(verb==='side-data-open'){dismissModal();model.section='privacy';refresh('side-section:privacy');return true;}
    if(verb==='side-section'){ruleModel.open=false;model.section=key;refresh(action);return true;}
    if(verb==='side-pref'){
      if(key==='blocklist'){ruleModel.open=true;refresh('side-rule-add');return true;}
      if(toggleKeys.has(key)){
        const next=!value(key);
        if(key==='danmaku')state.danmaku=next;
        else if(key==='preview')state.preview=next;
        else if(key==='motion'){state.reduced=next;document.querySelector('#stage').classList.toggle('reduced-motion',next);}
        else model[key]=next;
        refresh(action);return true;
      }
      if(key==='clearsearch'||key==='clearhistory'){
        modal(S.clearTitles[key],`<p>${e(S.clearBody)}</p>`,button(S.confirmClear,`side-clear:${key}`,'primary')+button(S.cancel,'dismissmodal','secondary'));return true;
      }
      const choices=choiceValues[key];
      modal(S.rows[key][0],`<div class="preference-options">${choices.map((label,index)=>`<button class="focusable preference-option ${value(key)===label?'selected':''}" data-action="side-choice:${key}:${index}"><span>${e(label)}</span>${value(key)===label?icon('check'):''}</button>`).join('')}</div>`,button(C.ui.close,'dismissmodal','secondary'));return true;
    }
    if(verb==='side-choice'){
      const index=Number(action.split(':')[2]);
      const choices=choiceValues[key];
      if(key==='subtitles')state.subtitles=choices[index];else model[key]=choices[index];
      if(key==='speed')state.speed=choices[index];
      if(key==='opacity')state.dmOpacity=parseInt(choices[index],10);
      if(key==='size')state.dmFontSize=index===0?29:36;
      if(key==='density')state.dmDensity=parseInt(choices[index],10);
      if(key==='area')state.dmArea=['upper','full','top'][index];
      dismissModal();refresh('side-pref:'+key);return true;
    }
    if(verb==='side-clear'){
      if(key==='clearhistory')state.history=[];
      else state.searchesCleared=true;
      dismissModal();refresh('side-pref:'+key);toast(S.cleared);return true;
    }
    if(verb==='side-login'){if(state.account)switchAccount();else login();return true;}
    if(verb==='side-membership'){rightsNotice('vip');return true;}
    if(verb==='side-library'){state.libraryTab=key;navigate('library',{parent:'account',parentFocus:action});requestAnimationFrame(()=>document.querySelector(`[data-action="library:${key}"]`)?.focus());return true;}
    if(verb==='side-sync'){model.section='privacy';navigate('settings',{parent:'account'});requestAnimationFrame(()=>document.querySelector('[data-action="side-pref:sync"]')?.focus());return true;}
    if(verb==='side-logout'){
      modal(A.logoutTitle,`<p>${e(A.logoutBody)}</p>`,button(A.logoutConfirm,'side-logout-confirm','primary')+button(A.cancel,'dismissmodal','secondary'));return true;
    }
    if(verb==='side-logout-confirm'){
      dismissModal();accountSignedOut();state.account=false;state.member=false;state.owned.clear();state.trial=null;state.trialEnded=false;state.scenario='guest';document.querySelector('#scenario-select').value='guest';navigate('account');return true;
    }
    return false;
  }
  return {settings,account,accountFocus,handle,preferenceRow,back(){if(state.page==='settings'&&ruleModel.open){ruleModel.open=false;refresh('side-pref:blocklist');return true;}return false;},preferences:model,rules,ruleModel,filterDanmaku:items=>items.filter(text=>!rules.some(rule=>rule.enabled&&rule.type==='keyword'&&text.includes(rule.text)))};
};
