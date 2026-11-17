/* A local discussion model for design review; no account requests are sent. */
window.createDiscussion = ({ content, icon, escape, button, refresh, openModal, dismissModal, toast, seek, editText }) => {
  const L = content.discussion;
  const model = { mode: 'panel', order: 'hot', root: null, votes: new Map(), listScroll: 0, returnAction: '', draft: null, posts: 0 };
  const number = value => Number(value || 0).toLocaleString('en-US');
  const roots = () => content.comments;
  const rootById = id => roots().find(item => item.id === id);
  const locate = id => roots().find(item => item.id === id) || roots().flatMap(item => item.replies || []).find(item => item.id === id);
  function avatar(item) {
    return item.own ? '<span class="discussion-avatar own-avatar">H</span>' : `<img class="discussion-avatar" src="assets/${escape(item.avatar)}" alt="">`;
  }
  function item(item, { root = false, preview = false } = {}) {
    const vote = model.votes.get(item.id);
    const count = (item.likes || 0) + (vote === 'up' ? 1 : 0);
    const rootId = root ? item.id : model.root;
    const expanded=!!model.root&&(root||model.activeReply===item.id);
    const selectable=!model.root||!root;
    const action=!model.root?`discussion-thread:${item.id}`:`discussion-select-reply:${item.id}`;
    const timestamp = item.timestamp&&expanded ? `<button class="comment-timestamp focusable" data-action="discussion-seek:${item.timestamp}" aria-label="${escape(L.timeLabel)} 09:42">09:42</button> ` : '';
    return `<article class="discussion-item ${root ? 'root-comment' : 'reply-comment'} ${selectable?'focusable comment-focus-row':''}" ${selectable?`tabindex="0" role="button" data-action="${action}"`:''} data-comment-id="${escape(item.id)}">${avatar(item)}<div class="discussion-content"><div class="discussion-author"><span>${escape(item.author)}</span>${item.up ? `<span class="up-label">${escape(L.up)}</span>` : ''}${item.pinned && !model.root ? `<span class="pinned-label">${icon('pin')}${escape(L.pinned)}</span>` : ''}</div><div class="discussion-meta">${escape(item.time)}${item.location ? ` · ${escape(item.location)}` : ''}</div><p class="discussion-text">${timestamp}${item.replyTo ? `<span class="reply-mention">${escape(L.replyTo)} @${escape(item.replyTo)}：</span>` : ''}${escape(item.text)}</p>${expanded?`<div class="discussion-actions"><button class="focusable comment-vote ${vote === 'up' ? 'selected' : ''}" data-action="discussion-vote:${item.id}:up" aria-label="${escape(L.like)} ${number(count)}" aria-pressed="${vote === 'up'}">${icon('like')}<span>${number(count)}</span></button><button class="focusable comment-vote ${vote === 'down' ? 'selected' : ''}" data-action="discussion-vote:${item.id}:down" aria-label="${escape(L.dislike)}" aria-pressed="${vote === 'down'}">${icon('dislike')}</button><button class="focusable comment-reply" data-action="discussion-compose:${rootId}:${item.id}">${escape(L.reply)}</button></div>`:`<div class="comment-summary-meta"><span>${icon('like')}${number(count)}</span><span>${icon('reply')}${item.replies?.length||0}</span></div>`}${preview && item.preview ? `<div class="reply-preview"><span class="preview-author">${escape(item.preview.author)}${item.preview.up ? `<i>${escape(L.up)}</i>` : ''}</span><span class="preview-text">${escape(item.preview.text)}</span></div>` : ''}${preview && item.replies?.length ? `<span class="view-replies">${escape(L.viewReplies)} ${item.replies.length} ${escape(L.replyCount)}</span>` : ''}</div></article>`;
  }
  function render(mode = model.mode) {
    model.mode = mode;
    const root = model.root ? rootById(model.root) : null;
    const total = number(2431 + model.posts);
    const heading = root ? `<button class="focusable discussion-back" data-action="discussion-back">${icon('back')}<span>${escape(L.allComments)}</span></button>` : `<h3>${escape(L.title)} <span>${total}</span></h3>`;
    const header = `<header class="discussion-header"><div class="discussion-heading">${heading}</div>${root ? `<div class="thread-count">${root.replies?.length || 0} ${escape(L.replyCount)}</div>` : `<div class="discussion-sort" role="tablist">${['hot', 'newest'].map(order => `<button class="focusable ${model.order === order ? 'selected' : ''}" data-action="discussion-sort:${order}" role="tab" aria-selected="${model.order === order}">${escape(L[order])}</button>`).join('')}</div>`}</header>`;
    let items;
    if (root) {
      items = `<div class="thread-original"><span class="thread-original-label">${escape(L.rootLabel)}</span>${item(root, { root: true })}</div><div class="thread-replies">${(root.replies || []).map(reply => item(reply)).join('')}</div>`;
    } else {
      const ordered = [...roots()];
      if (model.order === 'hot') ordered.sort((a,b) => Number(!!b.pinned) - Number(!!a.pinned) || b.likes - a.likes);
      else ordered.reverse();
      items = ordered.map(comment => item(comment, { root: true, preview: true })).join('');
    }
    const composer = `<footer class="discussion-composer"><span class="composer-avatar">H</span><button class="focusable" data-action="discussion-compose:${root?.id || 'new'}">${icon('comment')}<span>${escape(root ? `${L.replyTo} ${root.author}` : L.composer)}</span></button></footer>`;
    return `<section class="discussion ${root ? 'discussion-thread' : ''}">${header}<div class="discussion-scroll" tabindex="-1">${items}</div>${composer}</section>`;
  }
  function redraw(action = '', position = null) { refresh({ action, position, mode: model.mode }); }
  function compose(rootId, targetId) {
    const target = locate(targetId || rootId);
    model.draft = {root:rootId==='new'?null:rootId,target:target?.author||null,text:''};
    const title=target?`${L.replyTo} ${target.author}`:L.write;
    openModal(title,`<div class="comment-compose-author"><span class="composer-avatar">H</span><span>${escape(L.me)}</span></div><input id="discussion-draft" type="hidden" maxlength="500" value=""><div class="comment-draft-preview"><p class="comment-preview-copy">${escape(L.inputPlaceholder)}</p></div><span class="comment-character-count">0 / 500</span><p class="comment-input-error" aria-live="polite"></p>`,button(L.edit,'discussion-edit','secondary')+button(L.send,'discussion-send','primary','send','disabled'));
    document.querySelector('.state-panel .modal').classList.add('comment-preview-modal');
    requestAnimationFrame(()=>editText(document.querySelector('#discussion-draft'),{title,returnFocus:document.querySelector('[data-action="discussion-edit"]'),onCommit(){requestAnimationFrame(()=>document.querySelector('[data-action="discussion-send"]:not(:disabled)')?.focus());}}));
  }
  function onInput(event){
    if(event.target.id!=='discussion-draft')return false;
    model.draft.text=event.target.value;
    const copy=document.querySelector('.comment-preview-copy');if(copy)copy.textContent=model.draft.text||L.inputPlaceholder;
    const send=document.querySelector('[data-action="discussion-send"]');if(send)send.disabled=!model.draft.text.trim();
    const count=document.querySelector('.comment-character-count');if(count)count.textContent=`${[...model.draft.text].length} / 500`;
    return true;
  }

  function handle(action) {
    if (!action.startsWith('discussion-')) return false;
    const [verb, id, extra] = action.split(':');
    const scroller = document.querySelector('.discussion-scroll');
    if (verb === 'discussion-edit') {editText(document.querySelector('#discussion-draft'),{title:model.draft.target?`${L.replyTo} ${model.draft.target}`:L.write,returnFocus:document.querySelector('[data-action="discussion-edit"]'),onCommit(){requestAnimationFrame(()=>document.querySelector('[data-action="discussion-send"]:not(:disabled)')?.focus());}});return true;}
    if (verb === 'discussion-sort') { model.order = id; model.root = null; redraw(action, 0); }
    if (verb === 'discussion-select-reply') {model.activeReply=id;redraw(`discussion-compose:${model.root}:${id}`,scroller?.scrollTop||0);return true;}
    if (verb === 'discussion-thread') {model.activeReply=null; model.listScroll = scroller?.scrollTop || 0; model.returnAction = action; model.root = id; redraw('discussion-back', 0); }
    if (verb === 'discussion-back') back();
    if (verb === 'discussion-vote') { model.votes.set(id, model.votes.get(id) === extra ? null : extra); redraw(action, scroller?.scrollTop || 0); }
    if (verb === 'discussion-compose') compose(id, extra);
    if (verb === 'discussion-seek') seek(Number(id));
    if (verb === 'discussion-send') {
      const field = document.querySelector('#discussion-draft');
      const text = field?.value.trim();
      if (!text) { document.querySelector('.comment-input-error').textContent = L.empty; field?.setAttribute('aria-invalid','true'); document.querySelector('[data-action="discussion-edit"]')?.focus(); return true; }
      const post = { id: `local-${Date.now()}`, author: L.me, avatar: '', own: true, time: L.now, text: text.slice(0,500), likes: 0, replies: [], replyTo: model.draft.target };
      if (model.draft.root) rootById(model.draft.root)?.replies.push(post);
      else { roots().push(post); model.order = 'newest'; model.posts++; }
      dismissModal(); redraw(model.draft.root?`discussion-select-reply:${post.id}`:`discussion-thread:${post.id}`, model.draft.root ? null : 0); toast(L.posted);
    }
    return true;
  }
  function back() {
    if (!model.root) return false;
    model.root = null; redraw(model.returnAction, model.listScroll); return true;
  }
  function reset(mode) { model.mode = mode; model.root = null; }
  return { render, handle, back, reset, model, onInput };
};
