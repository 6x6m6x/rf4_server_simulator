// 发邮件：选人（可多选） → 写内容 → 加附件 → 发送。右边实时预览玩家看到的样子。
import { h, api, apiList, post, toast, errText, pageHead, section, btn, empty, debounce, searchBox, icon, badge, setLeaveGuard, confirmDialog, dialog } from '../core.js';
import { itemIcon, pickItem } from '../widgets.js';

const TEMPLATES = [
  ['补偿', '服务器补偿', '因为最近服务器的问题给你带来了不便，特此补偿，请查收附件。'],
  ['活动奖励', '活动奖励', '感谢参与本次活动！奖励已随邮件附上。'],
  ['欢迎', '欢迎来钓鱼', '欢迎来到我们的服务器！祝你钓到大鱼。'],
];

export default async function mail(params) {
  // 收件人：支持一次选多人；从玩家页带 params[0] 进入时预填一位。
  const recipients = [];
  // 从玩家页跳转可能带进 "null" 之类的脏参数，先校验再预填，避免拼出 nullnull。
  if (params[0] && params[0] !== 'null' && Number(params[0]) > 0) {
    try {
      const p = await api('/api/players/detail?uid=' + encodeURIComponent(params[0]));
      // detail 也可能返回空对象或空字段，校验后再加入收件人。
      if (p && Number(p.uid) > 0) recipients.push({ uid: p.uid, name: p.name || ('UID ' + p.uid), level: p.level });
    } catch (e) {}
  }

  const text = h('textarea', { class: 'textarea', rows: 9, placeholder: '写给玩家的话。可以只发附件、不写内容。', oninput: () => preview() });
  setLeaveGuard(() => !!(text.value.trim() || attach.length));

  // 收件人（可多选）
  const toBox = h('div');
  const results = h('div', { class: 'card', hidden: true, style: { marginTop: '6px', maxHeight: '240px', overflow: 'auto' } });
  const searchEl = searchBox('输入玩家昵称或 UID，点选加入收件人', debounce(async (q) => {
    if (!q.trim()) { results.hidden = true; return; }
    const list = await apiList('/api/players?q=' + encodeURIComponent(q.trim())).catch(() => []);
    results.hidden = false;
    results.replaceChildren(list.length ? h('div', { class: 'list' }, list.slice(0, 20).map((p) => h('div', {
      class: 'list-item clickable',
      onclick: () => {
        // 搜索结果也可能有空名字或无效 UID，同样兜底后再加入收件人。
        if (Number(p.uid) > 0 && !recipients.some((r) => r.uid === p.uid)) recipients.push({ uid: p.uid, name: p.name || ('UID ' + p.uid), level: p.level });
        searchInput.value = '';
        results.hidden = true;
        drawTo();
        preview();
      },
    }, h('div', { class: 'avatar', text: (p.name || '?').slice(0, 1) }),
      h('div', { class: 'grow' }, h('div', { class: 'title', text: p.name || ('UID ' + p.uid) }), h('div', { class: 'sub', text: `UID ${p.uid || '?'} · ${p.level} 级` })),
      p.online ? badge('在线', 'ok', true) : null))) : empty('没有找到'));
  }, 250));
  const searchInput = searchEl.querySelector('input');
  const searchWrap = h('div', {}, searchEl, results);

  const drawTo = () => {
    const list = recipients.length ? h('div', { class: 'card' }, h('div', { class: 'list' }, recipients.map((r, i) => h('div', { class: 'list-item' },
      h('div', { class: 'avatar', text: (r.name || '?').slice(0, 1) }),
      h('div', { class: 'grow' }, h('b', { text: r.name || ('玩家' + r.uid) }), h('div', { class: 'muted small', text: `UID ${r.uid || '?'}` + (r.level ? ` · ${r.level} 级` : '') })),
      btn('', () => { recipients.splice(i, 1); drawTo(); preview(); }, 'ghost icon sm', { icon: 'x', 'aria-label': '移除' }))))) : null;
    const actions = recipients.length ? h('div', { class: 'row', style: { marginTop: '8px', gap: '8px' } },
      btn('继续添加', () => { searchWrap.hidden = false; searchInput.focus(); }, 'ghost sm', { icon: 'plus' }),
      btn('清空', () => { recipients.length = 0; drawTo(); preview(); }, 'ghost sm')) : null;
    searchWrap.hidden = recipients.length > 0;
    toBox.replaceChildren(list, actions, searchWrap);
  };
  drawTo();

  // 附件
  const attach = [];
  const attachBox = h('div');
  const drawAttach = () => {
    attachBox.replaceChildren(attach.length ? h('div', { class: 'card' }, h('div', { class: 'list' }, attach.map((a, i) => h('div', { class: 'list-item' },
      itemIcon(a.item_id), h('div', { class: 'grow' }, h('div', { class: 'title', text: a.name }), h('div', { class: 'sub mono', text: a.item_id })),
      h('label', { class: 'row small muted' }, '数量', h('input', { class: 'input num', style: { width: '80px', height: '30px' }, value: a.qty, oninput: (e) => { a.qty = Math.max(1, Math.floor(+e.target.value || 1)); preview(); } })),
      btn('', () => { attach.splice(i, 1); drawAttach(); preview(); }, 'ghost icon sm', { icon: 'x', 'aria-label': '移除' }))))) : h('div', { class: 'muted small', text: '没有附件。' }));
  };
  drawAttach();

  // 预览
  const pv = h('div');
  function preview() {
    const lines = text.value.trim();
    const nameOf = (r) => r.name || ('玩家' + r.uid);
    const who = !recipients.length ? '还没选'
      : recipients.length <= 3 ? recipients.map((r) => `${nameOf(r)}（UID ${r.uid || '?'}）`).join('、')
      : `${recipients.slice(0, 3).map(nameOf).join('、')} 等 ${recipients.length} 人`;
    pv.replaceChildren(h('div', { class: 'card', style: { padding: '18px', background: 'var(--surface-2)' } },
      h('div', { class: 'muted small', text: '收件人：' + who }),
      h('div', { style: { margin: '12px 0', whiteSpace: 'pre-wrap', minHeight: '60px', color: lines ? 'var(--text)' : 'var(--faint)' }, text: lines || '（没有正文）' }),
      attach.length ? h('div', { class: 'row', style: { gap: '10px', borderTop: '1px solid var(--line)', paddingTop: '12px' } }, attach.map((a) => h('div', { class: 'row', style: { gap: '6px' } }, itemIcon(a.item_id, 28), h('span', { class: 'small', text: `${a.name} ×${a.qty}` })))) : null));
  }
  preview();

  // 邮件模板
  let templates = [];
  const tplBox = h('div');
  const templateItems = (t) => {
    try { const a = JSON.parse(t.attachments_json || '[]'); return Array.isArray(a) ? a : []; } catch (e) { return []; }
  };
  const applyTemplate = (t) => {
    text.value = t.body || t.subject || '';
    attach.length = 0;
    for (const it of templateItems(t)) {
      if (!it || !it.item_id || attach.some((a) => a.item_id === it.item_id)) continue;
      attach.push({ item_id: it.item_id, name: it.name || it.item_id, qty: Math.max(1, Math.floor(+it.qty || 1)) });
    }
    drawAttach();
    preview();
    toast('已载入模板「' + t.name + '」');
  };
  const removeTemplate = async (t) => {
    if (!(await confirmDialog({ title: `删除模板「${t.name}」？`, desc: '删除后无法恢复。', confirm: '删除', danger: true }))) return;
    try { await post('/api/mail/template/delete', { id: t.id }); toast('已删除模板'); await loadTemplates(); }
    catch (e) { toast('删除失败：' + errText(e), 'bad'); }
  };
  const drawTemplates = () => {
    tplBox.replaceChildren(templates.length ? h('div', { class: 'list' }, templates.map((t) => {
      const items = templateItems(t);
      return h('div', { class: 'list-item' },
        h('div', { class: 'grow clickable', onclick: () => applyTemplate(t) },
          h('div', { class: 'title', text: t.name }),
          h('div', { class: 'sub', text: (t.subject ? t.subject + ' · ' : '') + (t.body ? '有正文' : '仅附件') + (items.length ? ` · ${items.length} 个附件` : '') })),
        btn('', () => removeTemplate(t), 'ghost icon sm', { icon: 'trash', 'aria-label': '删除模板' }));
    })) : h('div', { class: 'muted small', text: '还没有保存的模板。点「写点什么」右上角的「存为模板」创建。' }));
  };
  const loadTemplates = async () => {
    try {
      const data = await api('/api/mail/templates');
      templates = Array.isArray(data.templates) ? data.templates : [];
    } catch (e) { templates = []; toast('加载模板失败：' + errText(e), 'bad'); }
    drawTemplates();
  };
  const saveTemplate = btn('存为模板', () => {
    const nameInput = h('input', { class: 'input', placeholder: '给模板起个名字', maxlength: '60' });
    const firstLine = (text.value.trim().split('\n')[0] || '').trim().slice(0, 120);
    const subjectInput = h('input', { class: 'input', value: firstLine, placeholder: '主题（选填）', maxlength: '200' });
    dialog({
      title: '存为模板',
      desc: '把当前正文和附件保存下来，之后在「我的模板」里一键载入。',
      body: h('div', { class: 'stack' },
        h('div', { class: 'field' }, h('label', { text: '模板名称' }), nameInput),
        h('div', { class: 'field' }, h('label', { text: '主题' }), subjectInput, h('div', { class: 'help', text: '主题只是备注，发送时不会单独展示；正文按当前编辑内容保存。' }))),
      actions: [
        { label: '取消', kind: 'ghost' },
        { label: '保存', kind: 'primary', onClick: async () => {
          const name = nameInput.value.trim();
          if (!name) { toast('请填写模板名称', 'bad'); return true; }
          try {
            await post('/api/mail/template/save', {
              id: 0, name,
              subject: subjectInput.value.trim().slice(0, 200),
              body: text.value.trim(),
              attachments_json: JSON.stringify(attach.map((a) => ({ item_id: a.item_id, name: a.name, qty: a.qty }))),
            });
            toast('模板已保存');
            await loadTemplates();
          } catch (e) { toast('保存失败：' + errText(e), 'bad'); }
        } },
      ],
    });
  }, 'ghost sm', { icon: 'doc' });
  loadTemplates();

  const send = btn('发送', async () => {
    if (!recipients.length) { toast('请先选择收件人', 'bad'); return; }
    if (!text.value.trim() && !attach.length) { toast('正文和附件不能都为空', 'bad'); return; }
    const title = recipients.length === 1 ? `发给「${recipients[0].name || ('玩家' + recipients[0].uid)}」？` : `发给 ${recipients.length} 位玩家？`;
    const desc = (attach.length ? `附带 ${attach.length} 种物品。` : '') + '发出后不能撤回。';
    if (!(await confirmDialog({ title, desc, confirm: '发送' }))) return;
    send.disabled = true;
    try {
      const r = await post('/api/mail/send', {
        uids: recipients.map((x) => x.uid),
        text: text.value.trim(),
        items: attach.map((a) => ({ item_id: a.item_id, qty: a.qty })),
      });
      toast(`已发送 ${r.queued || recipients.length} 封，约 2 秒内送达`);
      text.value = '';
      attach.length = 0;
      drawAttach();
      preview();
    } catch (e) { toast('发送失败：' + errText(e), 'bad'); }
    finally { send.disabled = false; }
  }, 'primary lg', { icon: 'mail' });

  return h('div', { class: 'page' },
    pageHead('发邮件', '给一个或多个玩家发消息，可以附带物品。玩家在游戏的邮箱里领取。'),
    h('div', { class: 'grid-2', style: { gridTemplateColumns: 'minmax(0,1.3fr) minmax(0,1fr)', alignItems: 'start' } },
      h('div', {},
        section({ title: '发给谁', desc: '可以搜索玩家逐个添加，一次选多人一起发。', body: toBox }),
        section({
          title: '写点什么',
          actions: h('div', { class: 'row', style: { gap: '6px', flexWrap: 'wrap' } },
            h('span', { class: 'muted small', text: '模板' }),
            ...TEMPLATES.map(([label, title, body]) => btn(label, () => { text.value = title + '\n\n' + body; preview(); }, 'ghost sm')),
            saveTemplate),
          body: text,
        }),
        section({ title: '附件', desc: '物品会和邮件一起送达，玩家领取后进背包。', actions: btn('添加物品', () => pickItem({ title: '添加附件', onPick: (x) => { if (!attach.some((a) => a.item_id === x.item_id)) attach.push({ ...x, qty: 1 }); drawAttach(); preview(); } }), 'sm', { icon: 'plus' }), body: attachBox }),
        section({ title: '我的模板', desc: '点一条载入到编辑区，右侧垃圾桶可删除。', body: tplBox })),
      h('div', { style: { position: 'sticky', top: '20px' } },
        section({ title: '预览', desc: '玩家收到的内容。', body: pv }),
        h('div', { class: 'row', style: { justifyContent: 'flex-end' } }, send))));
}
