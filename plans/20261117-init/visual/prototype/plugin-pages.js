'use strict';

window.createPluginPages = ({content:C,state,settings,themes,icon,escape:e,button,header,modal,dismissModal,navigate,renderPage,toast,login}) => {
  const seed = () => [
    {id:'sample-source',kind:'source',name:'示例影视',description:'视频、番剧与直播',glyph:'tv',artwork:{cover:'assets/entry-artwork/sample-source.png'},version:'1.0.0',latest:'1.0.0',repository:'随 MikuTV 提供',builtin:true,installed:true,enabled:true,permissions:['内容与账号服务']},
    {id:'video',kind:'source',name:'示例视频源',description:'视频与内容合集',glyph:'monitor',artwork:{cover:'assets/entry-artwork/video.png'},version:'1.2.0',latest:'1.3.0',repository:'demo/video-source',installed:true,enabled:true,permissions:['api.video.example'],additionalPermissions:['images.video.example']},
    {id:'media',kind:'source',name:'示例媒体源',description:'视频与播放列表',glyph:'playlist',artwork:{cover:'assets/concert.jpg'},version:'1.0.0',latest:'1.0.0',repository:'demo/media-source',installed:false,enabled:false,permissions:['media.example']},
    {id:'channel',kind:'source',name:'示例频道源',description:'频道与精选内容',glyph:'following',version:'2.0.0',latest:'2.0.0',repository:'demo/channel-source',installed:false,enabled:false,permissions:['channel.example'],incompatible:true},
    {id:'twilight',kind:'theme',name:'暮光紫',description:'柔和紫色与深色背景',glyph:'palette',version:'1.0.0',latest:'1.1.0',repository:'demo/twilight-theme',installed:true,palette:'twilight'},
    {id:'ocean',kind:'theme',name:'深海蓝',description:'清透蓝色与深海色调',glyph:'palette',version:'1.0.0',latest:'1.0.0',repository:'demo/ocean-theme',installed:false,palette:'ocean'}
  ];
  const model = {items:seed(),currentEntry:'sample-source',view:'installed',filter:'all',query:'',listScroll:0,catalogOrigin:null,selected:null,catalog:'ready',prompt:null,serial:0,timer:null,operationDelay:1800,failNext:false,lastFocus:'plugin-detail:sample-source',entryOrigin:null,forced:false,snapshots:{}};
  const sessionFields = ['account','member','scenario','owned','trialsUsed','trialEnded','later','favorites','likes','following','tracked','history','coins','query','searchKind','sort','searchesCleared','homeTab','category','libraryTab','subtitles','quality','speed','danmaku'];
  sessionFields.push('accountId','accountName','accountInitial');
  const accountFields=['account','member','scenario','owned','trialsUsed','trialEnded','later','favorites','likes','following','tracked','history','coins'];
  const accounts={};let addingAccount=false,accountSerial=0;
  const find = id => model.items.find(item=>item.id===id);
  const available = () => model.items.filter(item=>item.kind==='source'&&item.installed&&item.enabled);
  const active = () => state.page==='settings'&&settings.preferences.section==='plugins';
  const row = options => settings.preferenceRow(options);
  const focus = action => requestAnimationFrame(()=>[...document.querySelectorAll('[data-action]')].find(node=>node.dataset.action===action)?.focus({preventScroll:true}));
  const render = action => {renderPage();if(action)focus(action);};
  const intro = (title,note) => `<h2>${e(title)}</h2><p class="preference-intro">${e(note)}</p>`;
  const list = rows => `<div class="preference-list">${rows.join('')}</div>`;
  const breadcrumb = label => `<div class="rules-heading">${button(label,'plugin-back','text-back','back')}</div>`;
  const kindLabel = item => ({source:'内容源',theme:'主题'}[item.kind]||item.kind);
  document.addEventListener('error',event=>{
    if(event.target.matches?.('img[data-entry-cover]'))event.target.remove();
  },true);

  function status(item) {
    if(item.incompatible)return '需要更新应用';
    if(!item.installed)return '未安装';
    if(item.kind==='theme')return themes.model.current===item.id?'正在使用':'已安装';
    if(!item.enabled)return '已停用';
    return item.id===model.currentEntry?'当前入口':'已启用';
  }
  function pluginRow(item) {
    const update=item.installed&&item.version!==item.latest;
    return row({label:item.name,note:`${kindLabel(item)} · ${item.builtin?'内置插件':item.description} · ${item.version}${update?' · 有更新':''}`,glyph:item.glyph,visual:item.kind==='theme'?themes.swatch(item):'',value:status(item),action:'plugin-detail:'+item.id});
  }
  function renderDetail(item) {
    const rows=[];
    if(item.installed) {
      rows.push(item.kind==='theme'?row({label:'使用此主题',note:themes.model.current===item.id?'正在应用，停用将恢复内置主题。':'也可在设置的外观中选择。',glyph:'palette',toggle:themes.model.current===item.id,action:'plugin-toggle:'+item.id}):row({label:'启用插件',note:item.id===model.currentEntry?'正在使用此内容入口。':'启用后可从侧栏切换入口。',glyph:'plug',toggle:item.enabled,action:'plugin-toggle:'+item.id}));
      rows.push(row({label:'版本',note:item.builtin?'随 MikuTV 一同更新。':item.version!==item.latest?'可更新至 '+item.latest:'已是最新版本。',glyph:item.version!==item.latest?'refresh':'info',value:item.version,action:item.version!==item.latest?'plugin-update:'+item.id:''}));
    } else {
      rows.push(row({label:'版本',glyph:'info',value:item.version}));
    }
    rows.push(row({label:'来源',glyph:'repository',value:item.repository}));
    if(item.installed&&!item.builtin)rows.push(row({label:'卸载插件',note:item.kind==='theme'?(themes.model.current===item.id?'卸载后恢复内置主题。':'移除此主题，当前外观保持不变。'):'账号与设置保留。',glyph:'trash',action:'plugin-remove:'+item.id}));
    if(!item.installed&&!item.incompatible)rows.push(row({label:'安装插件',note:item.kind==='theme'?'安装后可在外观设置中选择。':'安装前查看所需权限。',glyph:'plus',action:'plugin-install:'+item.id}));
    return breadcrumb(model.view==='installed'?'插件管理':'发现插件')+intro(item.name,kindLabel(item)+' · '+(item.incompatible?'需要 MikuTV 1.1.0 或更高版本。':item.description))+list(rows);
  }
  function catalogState() {
    const labels={loading:['refresh','正在获取插件',''],empty:['plug','还没有可发现的插件','也可以从仓库地址安装。'],error:['wifi','暂时无法获取插件','请检查网络后重试。']};
    const details=labels[model.catalog];
    if(!details)return '';
    const [glyph,title,note]=details;
    return `<div class="rules-empty plugin-empty" role="status">${icon(glyph)}<h3>${e(title)}</h3>${note?`<p>${e(note)}</p>`:''}${model.catalog==='error'?button('重新加载','plugin-refresh','secondary','refresh'):model.catalog==='empty'?button('从仓库安装','plugin-add','secondary','repository'):''}</div>`;
  }
  function renderSettings() {
    if(model.selected)return renderDetail(find(model.selected));
    if(model.view==='discover'){
      const filters=[['all','全部'],['source','内容源'],['theme','主题']];
      const tabs=`<div class="plugin-kind-tabs" role="tablist" aria-label="Plugin kind">${filters.map(([id,label])=>button(label,'plugin-filter:'+id,'tab '+(model.filter===id?'selected':''),'',`role="tab" aria-selected="${model.filter===id}"`)).join('')}</div>`;
      const search=`<label class="plugin-search">${icon('search')}<input id="plugin-query" class="form-input focusable" value="${e(model.query)}" placeholder="搜索插件名称或仓库" aria-label="搜索插件" maxlength="120"></label>`;
      return breadcrumb(model.catalogOrigin==='appearance'?'主题选择':'已安装插件')+intro('发现插件',model.catalog==='offline'?'当前离线，显示缓存中可安装的插件。':'查找并安装新插件。')+search+tabs+`<div data-plugin-results>${catalogResults()}</div>`;
    }
    const installed=model.items.filter(item=>item.installed);
    return `<div class="section-heading"><div><h2>已安装插件</h2><p>管理内容源与主题。</p></div><div class="plugin-toolbar">${button('发现插件','plugin-discover','secondary','search')}${button('从仓库安装','plugin-add','secondary','repository')}</div></div>`+list([
      ...installed.map(pluginRow),
      row({label:'检查更新',glyph:'refresh',action:'plugin-refresh'})
    ]);
  }
  function catalogResults(){
    if(catalogState())return catalogState();
    const query=model.query.trim().toLocaleLowerCase();
    const items=model.items.filter(item=>!item.installed&&(model.filter==='all'||item.kind===model.filter)&&(!query||[item.name,item.description,item.repository].some(value=>value.toLocaleLowerCase().includes(query))));
    if(items.length)return list(items.map(pluginRow));
    return `<div class="rules-empty plugin-empty" role="status">${icon('search')}<h3>${query?'没有匹配的插件':'没有更多可安装的插件'}</h3><p>${query?'试试其他名称或仓库。':'已安装的插件可在插件管理中查看。'}</p>${button(query?'清除搜索':model.catalogOrigin==='appearance'?'返回主题选择':'返回已安装插件',query?'plugin-search-clear':'plugin-back','secondary')}</div>`;
  }
  function onInput(event){if(event.target.id!=='plugin-query')return false;model.query=event.target.value;document.querySelector('[data-plugin-results]').innerHTML=catalogResults();return true;}
  function returnToThemes(focusId){model.view='installed';model.catalogOrigin=null;model.selected=null;settings.preferences.section='appearance';render('theme-open');requestAnimationFrame(()=>themes.openPicker(focusId));}
  function snapshot() {
    const data={};for(const key of sessionFields)if(state[key]!==undefined)data[key]=structuredClone(state[key]);
    return {data,page:state.page,scroll:document.querySelector('#main').scrollTop,focus:state.contentFocus};
  }
  function openEntries({forced=false,removed=null,preferred=null}={}) {
    if(state.page==='entries')return;
    const current=model.currentEntry||removed;
    if(current)model.snapshots[current]=snapshot();
    model.entryOrigin={page:state.page,category:state.category,scroll:document.querySelector('#main').scrollTop,focus:document.activeElement?.dataset.action};
    model.forced=forced;
    if(forced)state.account=false;
    navigate('entries');
    focus(available().length?'entry-select:'+(available().find(item=>item.id===(preferred||model.currentEntry))||available()[0]).id:'entry-manage');
  }
  function renderEntries() {
    const current=find(model.currentEntry);
    const entries=available();
    const title=`<div class="page-title utility-title"><h1>选择入口</h1><p>${current?'当前入口 · '+e(current.name):'选择想看的内容来源。'}</p></div>`;
    const cards=`<div class="folder-grid entry-grid">${entries.map(item=>`<button class="folder-card focusable" data-action="entry-select:${item.id}" aria-current="${item.id===model.currentEntry?'true':'false'}"><div class="folder-cover"><span aria-hidden="true">${icon(item.glyph)}</span>${item.artwork?.cover?`<img class="entry-artwork" data-entry-cover src="${e(item.artwork.cover)}" alt="" decoding="async">`:''}${item.id===model.currentEntry?`<i class="entry-current">${icon('check')}当前入口</i>`:''}</div><strong>${e(item.name)}</strong><small>${e(item.description)}</small></button>`).join('')}</div>`;
    const empty=`<section class="collection-empty"><span>${icon('plug')}</span><h2>还没有可用入口</h2><p>先安装或启用一个内容插件。</p>${button('管理插件','entry-manage','secondary','plug')}</section>`;
    return header()+title+(entries.length?cards:empty);
  }
  function restoreRoute(saved,action) {
    navigate(saved.page);
    if(saved.category!==undefined){state.category=saved.category;renderPage();}
    requestAnimationFrame(()=>{document.querySelector('#main').scrollTo({top:saved.scroll||0,behavior:'instant'});focus(action||saved.focus||'entry-open');});
  }
  function chooseEntry(id) {
    if(!available().some(item=>item.id===id))return;
    if(id===model.currentEntry&&!model.forced){restoreRoute(model.entryOrigin||{page:'home'},model.entryOrigin?.focus);return;}
    const saved=model.snapshots[id];
    const defaults={account:false,accountId:null,accountName:'',accountInitial:'',member:false,scenario:'guest',owned:new Set(),trialsUsed:new Set(),trialEnded:false,later:new Set(),favorites:new Set(),likes:new Set(),following:new Set(),tracked:new Set(),history:[],coins:0,query:'',searchKind:'all',sort:0,searchesCleared:false,homeTab:'forYou',category:C.ui.any,libraryTab:'history',subtitles:C.ui.off,quality:'1080P',speed:'1.0x',danmaku:true};
    Object.assign(state,structuredClone({...defaults,...saved?.data}));
    model.currentEntry=id;model.forced=false;
    state.panel=null;state.scrub=null;state.trial=null;state.pendingAction=null;state.pendingQuality=null;state.creatorReturn=null;
    document.querySelector('#scenario-select').value=state.scenario;
    restoreRoute({page:saved?.page&&saved.page!=='entries'?saved.page:'home',scroll:saved?.scroll,category:saved?.data.category,focus:saved?.focus});
  }
  function leave() {
    clearTimeout(model.timer);model.timer=null;model.serial++;model.prompt=null;
    if(model.catalog==='loading')model.catalog='ready';
  }
  function cancelPrompt() {
    clearTimeout(model.timer);model.timer=null;model.serial++;model.prompt=null;dismissModal();
  }
  function showPrompt(kind,id) {
    const item=find(id);if(!item)return;
    const version=kind==='install'||kind==='update'?item.latest:item.version;
    model.prompt={kind,id,version};
    const update=kind==='update';
    const permissions=[...(item.permissions||[]),...(update?item.additionalPermissions||[]:[])];
    const titles={install:'安装插件？',update:'更新插件？',disable:'停用插件？',remove:'卸载插件？'};
    const labels={install:'安装插件',update:'更新并允许',disable:'停用插件',remove:'卸载插件'};
    let body=`<p class="dialog-context">${e(item.name)} · ${update?e(item.version)+' → ':''}${e(version)}</p>`;
    if(item.kind==='theme'){
      body+=`<p>${kind==='install'?'安装后可在外观设置中选择，当前主题保持不变。':update?'更新已安装的主题。':themes.model.current===item.id?'将恢复内置主题「 MikuTV Dark」。':'当前外观保持不变。'}</p>`;
      if(update)labels.update='更新主题';
    }else if(kind==='install'||update)body+=`<p>允许此插件访问以下服务。</p><div class="preference-options plugin-permissions">${permissions.map(domain=>`<div class="preference-option"><span>${e(domain)}</span>${update&&item.additionalPermissions?.includes(domain)?'<small>新增权限</small>':icon('lock')}</div>`).join('')}</div>`;
    else body+=`<p>${kind==='remove'?'此插件将从设备移除。':'停用后，入口将不再显示。'}<br>账号与设置保留。${item.id===model.currentEntry?'<br>你需要重新选择入口。':''}</p>`;
    modal(titles[kind],`<div data-plugin-dialog="${kind}">${body}</div>`,button(labels[kind],'plugin-confirm','primary')+button('取消','plugin-cancel','secondary'));
  }
  function failOperation(request) {
    dismissModal();model.prompt={...request,phase:'error'};
    modal(request.kind==='update'?'更新未完成':'安装未完成',`<div data-plugin-dialog="error"><p>连接中断。${request.kind==='update'?'当前版本可以继续使用。':'插件尚未安装。'}<br>网络恢复后可以重试。</p></div>`,button('重试','plugin-retry','primary','refresh')+button('稍后再试','plugin-cancel','secondary'));
  }
  function startOperation(request) {
    dismissModal();
    const item=find(request.id),serial=++model.serial;
    model.prompt={...request,phase:'pending'};
    modal(request.kind==='update'?'正在更新插件':'正在安装插件',`<div data-plugin-dialog="pending"><p class="dialog-context">${e(item.name)} · ${e(request.version)}</p><div class="plugin-download" role="progressbar" aria-label="Package verification"><i></i></div><p>正在下载并检查插件包…</p></div>`,button('取消','plugin-cancel','secondary'));
    model.timer=setTimeout(()=>{
      if(serial!==model.serial||!active()||model.prompt?.phase!=='pending')return;
      if(model.failNext){model.failNext=false;failOperation(request);return;}
      item.installed=true;if(request.kind==='install'&&item.kind==='source')item.enabled=true;item.version=request.version;
      const returnToPicker=item.kind==='theme'&&model.catalogOrigin==='appearance';
      dismissModal();leave();
      if(returnToPicker){returnToThemes(item.id);return;}
      model.view='installed';model.selected=item.kind==='theme'?item.id:null;render(item.kind==='theme'?'plugin-toggle:'+item.id:'plugin-detail:'+item.id);
      if(request.kind==='install'&&item.kind==='source'){
        requestAnimationFrame(()=>modal('插件已安装',`<div data-plugin-installed="${e(item.id)}"><p class="dialog-context">${e(item.name)}</p><p>已添加到入口列表。选择入口后开始浏览，当前入口保持不变。</p></div>`,button('选择入口','plugin-open-entry:'+item.id,'primary','switch')+button('完成','plugin-install-done','secondary')));
        return;
      }
      toast(request.kind==='update'?'插件已更新':'插件已安装');
    },model.operationDelay);
  }
  function confirm() {
    const request=model.prompt;if(!request||request.phase)return;
    if(request.kind==='install'||request.kind==='update'){startOperation(request);return;}
    const item=find(request.id),wasCurrent=model.currentEntry===item.id;
    if(item.kind==='theme'){if(themes.model.current===item.id)themes.apply('mikutv-dark');}
    else item.enabled=false;
    if(request.kind==='remove')item.installed=false;
    dismissModal();leave();
    if(wasCurrent){model.currentEntry=null;openEntries({forced:true,removed:item.id});}
    else {if(request.kind==='remove')model.selected=null;render(model.selected?'plugin-toggle:'+item.id:'plugin-detail:'+model.items.find(value=>value.installed)?.id);}
  }
  function addRepository() {
    model.prompt={kind:'repository'};
    modal('从仓库安装','<div data-plugin-dialog="repository"><label class="field-label" for="plugin-repository">GitHub 仓库地址</label><input id="plugin-repository" class="form-input focusable" placeholder="owner/repository" maxlength="120"><p class="form-error" id="plugin-repository-error" role="alert"></p></div>',button('查看插件','plugin-resolve','primary')+button('取消','plugin-cancel','secondary'));
  }
  function resolveRepository() {
    const value=document.querySelector('#plugin-repository').value.trim().replace(/^https:\/\/github.com\//,'').replace(/\/$/,'');
    const error=document.querySelector('#plugin-repository-error');
    if(!/^[\w.-]+\/[\w.-]+$/.test(value)){error.textContent='请输入有效的 GitHub 仓库地址';return;}
    const item=model.items.find(valueItem=>!valueItem.builtin&&valueItem.repository===value);
    if(!item){error.textContent='未找到可安装的正式版本';return;}
    cancelPrompt();model.view='discover';model.filter='all';model.catalogOrigin=null;model.selected=item.id;render(item.installed?'plugin-toggle:'+item.id:'plugin-install:'+item.id);
  }
  function refreshCatalog() {
    if(model.view==='installed'){const count=model.items.filter(item=>item.installed&&item.version!==item.latest).length;toast(count?'发现 '+count+' 个可更新插件':'所有插件均为最新版本');return;}
    const serial=++model.serial;model.catalog='loading';render('plugin-back');
    model.timer=setTimeout(()=>{if(serial!==model.serial||!active()||model.view!=='discover')return;model.catalog=model.failNext?'offline':'ready';model.failNext=false;render('plugin-filter:'+model.filter);},model.operationDelay);
  }
  function back() {
    if(state.page==='entries') {
      if(model.forced||!model.currentEntry){focus(available().length?'entry-select:'+available()[0].id:'entry-manage');return true;}
      restoreRoute(model.entryOrigin||{page:'home'});return true;
    }
    if(!active())return false;
    if(model.prompt){cancelPrompt();return true;}
    if(state.overlay)return false;
    if(model.selected){const id=model.selected;model.selected=null;render('plugin-detail:'+id);requestAnimationFrame(()=>document.querySelector('#main').scrollTop=model.listScroll);return true;}
    if(model.view==='discover'){leave();if(model.catalogOrigin==='appearance')returnToThemes();else{model.view='installed';render('plugin-discover');}return true;}
    return false;
  }
  function handle(action) {
    if(action?.startsWith('side-section:')&&action!=='side-section:plugins'){leave();model.catalogOrigin=null;return false;}
    if(action==='entry-open'){openEntries();return true;}
    if(action==='entry-manage'){settings.preferences.section='plugins';model.view='installed';model.selected=null;navigate('settings');focus('side-section:plugins');return true;}
    if(action?.startsWith('entry-select:')){chooseEntry(action.split(':')[1]);return true;}
    if(action==='account-switch'){openAccounts();return true;}
    if(action==='account-add'){addingAccount=true;dismissModal();login();return true;}
    if(action?.startsWith('account-select:')){selectAccount(action.split(':')[1]);return true;}
    if(!action?.startsWith('plugin-'))return false;
    const [verb,id]=action.split(':');
    if(verb==='plugin-detail'){model.listScroll=document.querySelector('#main').scrollTop;model.selected=id;render(find(id).installed?'plugin-toggle:'+id:find(id).incompatible?'plugin-back':'plugin-install:'+id);}
    if(verb==='plugin-back')back();
    if(verb==='plugin-discover'){model.view='discover';model.filter='all';model.query='';model.catalogOrigin=null;model.catalog='ready';render('plugin-filter:all');}
    if(verb==='plugin-filter'){model.filter=id;render(action);}
    if(verb==='plugin-open-entry'){dismissModal();openEntries({preferred:id});}
    if(verb==='plugin-install-done')dismissModal();
    if(verb==='plugin-search-clear'){model.query='';render();requestAnimationFrame(()=>document.querySelector('#plugin-query')?.focus({preventScroll:true}));}
    if(verb==='plugin-install'||verb==='plugin-update'||verb==='plugin-remove')showPrompt(verb.slice(7),id);
    if(verb==='plugin-toggle'){const item=find(id);if(item.kind==='theme'){if(themes.model.current===id)showPrompt('disable',id);else if(themes.apply(id))render(action);}else if(item.enabled)showPrompt('disable',id);else{item.enabled=true;render(action);}}
    if(verb==='plugin-confirm')confirm();
    if(verb==='plugin-cancel')cancelPrompt();
    if(verb==='plugin-retry')startOperation(model.prompt);
    if(verb==='plugin-add')addRepository();
    if(verb==='plugin-resolve')resolveRepository();
    if(verb==='plugin-refresh')refreshCatalog();
    return true;
  }
  function handleKey(event) {
    if(event.key==='Escape'&&back()){event.preventDefault();return true;}
    if(!active()||state.overlay)return false;
    if(event.key==='ArrowRight'&&document.activeElement?.dataset.action==='side-section:plugins') {
      event.preventDefault();
      const target=[...document.querySelectorAll('.preferences [data-action]')].find(node=>node.dataset.action===model.lastFocus)||document.querySelector('.preferences [data-action]');
      target?.focus({preventScroll:true});return true;
    }
    if(event.key==='ArrowLeft'&&document.activeElement?.closest('.preferences')){event.preventDefault();focus('side-section:plugins');return true;}
    return false;
  }
  function onFocus(node) {if(active()&&node.closest('.preferences')&&node.dataset.action)model.lastFocus=node.dataset.action;}
  function preview(name='installed') {
    if(state.overlay==='modal')dismissModal();leave();
    Object.assign(model,{items:seed(),currentEntry:'sample-source',view:'installed',filter:'all',query:'',listScroll:0,catalogOrigin:null,selected:null,catalog:'ready',prompt:null,failNext:false,snapshots:{},entryOrigin:null,forced:false,lastFocus:'plugin-detail:sample-source'});
    themes.model.failNext=false;themes.apply('mikutv-dark');
    for(const id of Object.keys(accounts))delete accounts[id];addingAccount=false;state.accountId=null;state.accountName='';state.accountInitial='';
    state.account=true;state.member=false;state.scenario='free';settings.preferences.section='plugins';navigate('settings');
    if(name==='entries'||name==='entries-empty'){if(name==='entries-empty'){model.items.forEach(item=>item.enabled=false);model.currentEntry=null;}openEntries({forced:name==='entries-empty'});return;}
    if(['discover','offline','loading','empty','error'].includes(name)){model.view='discover';model.catalog=name==='discover'?'ready':name;render(name==='error'?'plugin-refresh':name==='empty'?'plugin-add':name==='loading'?'plugin-back':'plugin-detail:media');return;}
    if(name==='detail'||name==='external'){model.selected=name==='detail'?'sample-source':'video';render('plugin-toggle:'+model.selected);return;}
    if(name==='repository'){render('plugin-add');requestAnimationFrame(addRepository);return;}
    if(['install','update','disable','remove','failed','progress'].includes(name)) {
      const id=name==='install'||name==='progress'?'media':name==='disable'?'sample-source':'video';
      model.view=id==='media'?'discover':'installed';model.selected=id;render('plugin-'+(id==='media'?'install':name==='disable'?'toggle':'update')+':'+id);
      requestAnimationFrame(()=>{
        const kind=name==='failed'?'update':name==='progress'?'install':name;
        showPrompt(kind,id);
        if(name==='failed'){failOperation({kind:'update',id,version:find(id).latest});}
        if(name==='progress')startOperation(model.prompt);
      });return;
    }
    render(name==='sidebar'?'entry-open':'plugin-detail:sample-source');
  }
  function openThemes(){model.view='discover';model.filter='theme';model.query='';model.catalogOrigin='appearance';model.selected=null;model.catalog='ready';settings.preferences.section='plugins';navigate('settings');focus('plugin-filter:theme');}
  function accountData(){const data={account:false,member:false,scenario:'guest',owned:new Set(),trialsUsed:new Set(),trialEnded:false,later:new Set(),favorites:new Set(),likes:new Set(),following:new Set(),tracked:new Set(),history:[],coins:0};for(const key of accountFields)if(state[key]!==undefined)data[key]=structuredClone(state[key]);return data;}
  function accountList(){
    const list=accounts[model.currentEntry]||(accounts[model.currentEntry]=[]);
    if(state.account){let item=list.find(item=>item.id===state.accountId);if(!item){item={id:'account-'+(++accountSerial),name:state.accountName||C.nav.account,initial:state.accountInitial||'H',data:accountData()};list.push(item);state.accountId=item.id;}else item.data=accountData();}
    return list;
  }
  function openAccounts(){
    const list=accountList();
    modal('切换账号',`<div data-account-picker><p>${e(find(model.currentEntry).name)} · 已登录的账号</p><div class="preference-options">${list.map(item=>`<button class="focusable preference-option ${item.id===state.accountId?'selected':''}" data-action="account-select:${item.id}" role="radio" aria-checked="${item.id===state.accountId}"><span>${e(item.name)}</span>${item.id===state.accountId?icon('check'):''}</button>`).join('')}</div></div>`,button('添加账号','account-add','secondary','plus'));
    if(state.accountId)focus('account-select:'+state.accountId);
  }
  function selectAccount(id){
    const item=accountList().find(item=>item.id===id);if(!item)return;
    Object.assign(state,structuredClone(item.data),{account:true,accountId:item.id,accountName:item.name,accountInitial:item.initial});
    document.querySelector('#scenario-select').value=state.scenario;dismissModal();render('side-login');
  }
  function completeAccountLogin(){
    if(addingAccount){
      const list=accounts[model.currentEntry]||(accounts[model.currentEntry]=[]);
      const item={id:'account-'+(++accountSerial),name:'观影账号 '+(list.length+1),initial:'M',data:{account:true,member:false,scenario:'free',owned:new Set(),trialsUsed:new Set(),trialEnded:false,later:new Set(),favorites:new Set(),likes:new Set(),following:new Set(),tracked:new Set(),history:[],coins:0}};
      list.push(item);Object.assign(state,structuredClone(item.data),{accountId:item.id,accountName:item.name,accountInitial:item.initial});addingAccount=false;
    }else accountList();
  }
  function cancelAccountLogin(){addingAccount=false;}
  function accountSignedOut(){const list=accounts[model.currentEntry]||[];const index=list.findIndex(item=>item.id===state.accountId);if(index>=0)list.splice(index,1);state.accountId=null;state.accountName='';state.accountInitial='';}
  return {model,renderSettings,renderEntries,openEntries,openThemes,openAccounts,completeAccountLogin,cancelAccountLogin,accountSignedOut,handle,handleKey,onInput,onFocus,leave,preview};
};
