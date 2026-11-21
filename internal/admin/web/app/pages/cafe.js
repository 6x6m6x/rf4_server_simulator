// 咖啡厅订单：悬赏赏金改金币，按鱼种稀有度 / 奖杯 / 蓝冠 / 巨物分档定价。
// 配置在表格里直接改、底部统一保存；黑名单与候选池为即时操作。
import {
  h, api, post, toast, errText, pageHead, section, callout, badge, btn, empty,
  Form, savebar, setLeaveGuard, searchBox, debounce, confirmDialog, render,
} from '../core.js';

const BAND_LABEL = {
  normal_common: '普通/达标 · 常见', normal_rare: '普通/达标 · 稀有',
  trophy_common: '奖杯 · 常见', trophy_rare: '奖杯 · 稀有',
  blue_common: '蓝冠 · 常见', blue_rare: '蓝冠 · 稀有',
};
const QTY_LABEL = {
  common_normal: '普通鱼', common_qualified: '达标鱼',
  rare_normal: '稀有普通鱼', rare_qualified: '稀有达标鱼',
};
const PERIOD_LABEL = { longterm: '长期位', trophy: '奖杯位', normal: '普通/达标位' };
const MIX_LABELS = [['trophy', '奖杯位'], ['longterm', '长期位'], ['normal', '普通/达标位']];

// 界面单位：金（1 金 = 100 分）；周期界面单位：小时（后端存秒）
const gold = (cents) => String(Number(cents || 0) / 100);
const hours = (seconds) => String(Number(seconds || 0) / 3600);

export default async function cafe() {
  const [cfg, poolInfo] = await Promise.all([api('/api/cafe/config'), api('/api/cafe/pool')]);
  const levels = poolInfo.levels || [];
  let fishHits = [];

  const form = new Form();
  const num = (key, value, spec = {}) => {
    const f = form.field({ key, label: key, kind: 'float', min: 0, ...spec }, value);
    const input = h('input', { class: 'input num', style: { width: '104px' }, oninput: () => form.refresh() });
    input.value = f.input ? f.input.value : f.original;
    f.input = input;
    f.row = input;
    return input;
  };
  const cellNum = (key, value, spec) => {
    const input = num(key, value, spec);
    return h('td', {}, input);
  };
  const changed = (key) => form.dirtyKeys().includes(key);

  savebar(form, {
    onSave: async (values, keys) => {
      const by = (prefix) => keys.filter((k) => k.startsWith(prefix));
      const bandKeys = new Set(by('band|').map((k) => k.split('|')[1]));
      for (const key of bandKeys) {
        const loC = Math.round(Number(values['band|' + key + '|lo']) * 100);
        const hiC = Math.round(Number(values['band|' + key + '|hi']) * 100);
        if (!Number.isFinite(loC) || !Number.isFinite(hiC) || loC < 0 || hiC < loC) throw new Error('金币范围无效（下限须 ≤ 上限）');
        await post('/api/cafe/band', { key, lo_cents: loC, hi_cents: hiC });
      }
      // 巨物补贴：sub|grp|min / lo / hi
      const subKeys = new Set(by('sub|').map((k) => k.split('|')[1]));
      for (const grp of subKeys) {
        const min = Math.round(Number(values['sub|' + grp + '|min']) || 0);
        const loC = Math.round(Number(values['sub|' + grp + '|lo']) * 100);
        const hiC = Math.round(Number(values['sub|' + grp + '|hi']) * 100);
        if (!Number.isFinite(loC) || !Number.isFinite(hiC) || loC < 0 || hiC < loC) throw new Error('补贴范围无效');
        await post('/api/cafe/subsidy', { grp, min_blue_g: min, lo_cents: loC, hi_cents: hiC });
      }
      // 交单数量
      for (const key of by('qty|')) {
        const quantity = parseInt(values[key], 10);
        if (!Number.isFinite(quantity) || quantity < 1 || quantity > 1000) throw new Error('交单数量须为 1 ~ 1000');
        await post('/api/cafe/quantity', { qkey: key.slice(4), quantity });
      }
      // 轮换周期
      const perKeys = new Set(by('period|').map((k) => k.split('|')[1]));
      for (const kind of perKeys) {
        const minS = Math.round(Number(values['period|' + kind + '|min']) * 3600);
        const maxS = Math.round(Number(values['period|' + kind + '|max']) * 3600);
        if (!Number.isFinite(minS) || !Number.isFinite(maxS) || minS <= 0 || maxS < minS) throw new Error('轮换周期无效');
        await post('/api/cafe/period', { kind, min_seconds: minS, max_seconds: maxS });
      }
      // 槽位占比：三类合计必须 10
      const mixKeys = by('mix|');
      if (mixKeys.length) {
        const counts = {};
        for (const k of mixKeys) counts[k.slice(4)] = parseInt(values[k], 10) || 0;
        if (counts.trophy + counts.longterm + counts.normal !== 10) throw new Error('三类槽位数量合计必须为 10');
        await post('/api/cafe/slot-mix', counts);
      }
      toast('已保存，候选池正在重建');
      setTimeout(render, 400);
    },
  });
  setLeaveGuard(() => form.isDirty());

  const mixInputs = {};
  const mixHint = h('div', { class: 'small text-2', style: { marginTop: '8px' } });
  const updateMixHint = () => {
    const total = MIX_LABELS.reduce((a, [k]) => a + (Number(mixInputs[k] && mixInputs[k].value) || 0), 0);
    mixHint.textContent = '当前合计 ' + total + ' / 10' + (total === 10 ? ' ✓' : '（必须为 10）');
    mixHint.style.color = total === 10 ? '' : 'var(--bad)';
  };

  const table = (head, body) => h('div', { class: 'table-wrap' }, h('table', { class: 'table' },
    h('thead', {}, h('tr', {}, ...head.map((t) => h('th', { text: t })))), h('tbody', {}, ...body)));

  // ---------- 金币范围 ----------
  const bandRows = (cfg.bands || []).map((b) => h('tr', {},
    h('td', {}, h('b', { text: BAND_LABEL[b.key] || b.label || b.key })),
    cellNum('band|' + b.key + '|lo', gold(b.lo_cents), { min: 0 }),
    cellNum('band|' + b.key + '|hi', gold(b.hi_cents), { min: 0 })));
  const bands = h('div', {}, table(['档位', '下限（金）', '上限（金）'], bandRows));

  // ---------- 巨物补贴 ----------
  const subRows = (cfg.subsidies || []).map((s) => h('tr', {},
    h('td', {}, h('b', { text: String(s.label || s.grp).split('（')[0] })),
    cellNum('sub|' + s.grp + '|min', s.min_blue_g, { kind: 'int', min: 0 }),
    cellNum('sub|' + s.grp + '|lo', gold(s.lo_cents), { min: 0 }),
    cellNum('sub|' + s.grp + '|hi', gold(s.hi_cents), { min: 0 })));
  const subs = h('div', {}, table(['重量级', '蓝冠线起（g）', '下限（金）', '上限（金）'], subRows));

  // ---------- 交单数量 ----------
  const qtyRows = (cfg.quantities || []).map((q) => h('tr', {},
    h('td', {}, h('b', { text: QTY_LABEL[q.qkey] || q.label || q.qkey })),
    cellNum('qty|' + q.qkey, q.quantity, { kind: 'int', min: 1, max: 1000 })));
  const qty = h('div', {}, table(['档位', '需交（条）'], qtyRows));

  // ---------- 轮换周期 ----------
  const periodRows = (cfg.periods || []).map((p) => h('tr', {},
    h('td', {}, h('b', { text: PERIOD_LABEL[p.kind] || p.kind })),
    cellNum('period|' + p.kind + '|min', hours(p.min_seconds), { min: 0.01 }),
    cellNum('period|' + p.kind + '|max', hours(p.max_seconds), { min: 0.01 })));
  const periods = h('div', {}, table(['槽位', '最短（时）', '最长（时）'], periodRows));

  // ---------- 槽位占比 ----------
  const mixFields = MIX_LABELS.map(([kind, label]) => {
    const row = (cfg.slot_mix || []).find((m) => m.kind === kind);
    const input = num('mix|' + kind, row ? row.count : 0, { kind: 'int', min: 0, max: 10, oninput: undefined });
    input.addEventListener('input', updateMixHint);
    mixInputs[kind] = input;
    return h('div', { class: 'field' }, h('label', { text: label }), input);
  });
  const mix = h('div', {}, h('div', { class: 'grid-3' }, ...mixFields), mixHint);
  updateMixHint();

  // ---------- 按水域黑名单（即时操作） ----------
  const blLevel = h('select', { class: 'select', onchange: () => drawBlacklist() },
    h('option', { value: '', text: '请选择水域' }), ...levels.map((l) => h('option', { value: l, text: l })));
  const blSearchWrap = searchBox('输入鱼种中文名或 key 搜索');
  const blSearchInput = blSearchWrap.querySelector('input');
  const blHits = h('div', { class: 'row', style: { flexWrap: 'wrap', marginTop: '10px' } });
  const blBody = h('tbody');
  const blacklist = h('div', { class: 'stack' },
    h('div', { class: 'row' }, blLevel, h('div', { class: 'grow', style: { maxWidth: '360px' } }, blSearchWrap)),
    blHits, h('div', { class: 'table-wrap' }, h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, h('th', { text: '水域' }), h('th', { text: '鱼种 key' }), h('th', { text: '中文名' }), h('th', {}))), blBody)));

  const runSearch = debounce(async (v) => {
    const q = v.trim();
    if (!q) { fishHits = []; blHits.replaceChildren(); return; }
    try {
      const d = await api('/api/cafe/fish/search?q=' + encodeURIComponent(q));
      fishHits = d.items || [];
      blHits.replaceChildren(...(fishHits.length
        ? fishHits.map((f, i) => h('span', { class: 'pill alt', style: { cursor: 'pointer' }, onclick: () => addBlacklist(i) },
          f.name_zh, ' ', h('span', { class: 'muted small', text: f.fish_key })))
        : [h('span', { class: 'muted small', text: '无匹配鱼种' })]));
    } catch (e) { blHits.replaceChildren(h('span', { class: 'muted small', text: '搜索失败：' + errText(e) })); }
  }, 180);
  blSearchInput.addEventListener('input', (e) => runSearch(e.target.value));

  async function addBlacklist(idx) {
    const f = fishHits[idx];
    const level = blLevel.value;
    if (!f || !level) { toast('请先选择水域', 'bad'); return; }
    try {
      await post('/api/cafe/blacklist/add', { level, fish_key: f.fish_key, name_zh: f.name_zh });
      toast('已加入黑名单，候选池正在重建');
      blSearchInput.value = ''; blHits.replaceChildren(); fishHits = [];
      await refreshBlacklist();
    } catch (e) { toast(errText(e), 'bad'); }
  }
  async function removeBlacklist(level, key, name) {
    if (!(await confirmDialog({ title: '移除黑名单', desc: `从「${level}」移除「${name || key}」？`, confirm: '移除', danger: true }))) return;
    try { await post('/api/cafe/blacklist/remove', { level, fish_key: key }); toast('已移除'); await refreshBlacklist(); }
    catch (e) { toast(errText(e), 'bad'); }
  }
  async function refreshBlacklist() {
    const fresh = await api('/api/cafe/config');
    cfg.blacklist = fresh.blacklist || [];
    drawBlacklist();
  }
  function drawBlacklist() {
    const level = blLevel.value;
    const rows = (cfg.blacklist || []).filter((b) => b.level === level);
    blBody.replaceChildren(...(rows.length
      ? rows.map((b) => h('tr', {},
        h('td', { text: b.level }), h('td', { class: 'mono', text: b.fish_key }), h('td', { text: b.name_zh }),
        h('td', {}, btn('移除', () => removeBlacklist(b.level, b.fish_key, b.name_zh), 'ghost sm'))))
      : [h('tr', {}, h('td', { colspan: 4 }, empty(level ? '该水域暂无黑名单' : '请先选择水域')))]));
  }
  drawBlacklist();

  // ---------- 候选池预览 ----------
  const poolLevel = h('select', { class: 'select', style: { width: '200px' }, onchange: () => loadPool() },
    h('option', { value: '', text: '全部水域' }), ...levels.map((l) => h('option', { value: l, text: l })));
  const poolCount = h('span', { class: 'muted small' });
  const poolBody = h('tbody');
  const poolTable = h('div', { class: 'table-wrap', style: { maxHeight: 'calc(100vh - 300px)' } },
    h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, ['水域', '槽位', '鱼种', '档位', '重量(g)', '需交(条)', '稀有', '巨物', '补贴(金)', '悬赏(金)'].map((t) => h('th', { text: t })))),
      poolBody));

  async function loadPool() {
    const lv = poolLevel.value;
    try {
      const data = await api('/api/cafe/pool' + (lv ? '?level=' + encodeURIComponent(lv) : ''));
      const rows = data.rows || [];
      poolCount.textContent = `${rows.length} 条候选`;
      poolBody.replaceChildren(...(rows.length
        ? rows.slice(0, 300).map((r) => h('tr', {},
          h('td', { text: r.level }), h('td', { text: r.slot }),
          h('td', {}, h('div', {}, h('b', { text: r.name_zh || '' }), h('div', { class: 'sub mono', text: r.species }))),
          h('td', {}, badge(r.grade || '', r.grade === 'super' ? 'accent' : r.grade === 'trophy' ? 'warn' : '')),
          h('td', { class: 'num', text: String(r.weight_g) }), h('td', { class: 'num', text: String(r.quantity || 1) }),
          h('td', {}, r.is_rare ? badge('稀有', 'warn') : null), h('td', {}, r.is_giant ? badge('巨物', 'accent') : null),
          h('td', { class: 'num', text: (Number(r.subsidy_cents) / 100).toFixed(1) }),
          h('td', { class: 'num' }, h('b', { text: (Number(r.gold_cents) / 100).toFixed(2) }))))
        : [h('tr', {}, h('td', { colspan: 10 }, empty('暂无候选订单', '首次启动约 2 秒后自动生成')))]));
      if (rows.length > 300) poolBody.append(h('tr', {}, h('td', { colspan: 10, class: 'muted small', text: '仅显示前 300 条，共 ' + rows.length + ' 条' })));
    } catch (e) { poolBody.replaceChildren(h('tr', {}, h('td', { colspan: 10 }, empty('读取失败', errText(e))))); }
  }
  async function regenPool() {
    if (!(await confirmDialog({ title: '重建候选池', desc: '重建全部水域的候选订单？当前在线订单在下个周期切换后使用新池。', confirm: '重建', danger: true }))) return;
    try { await post('/api/cafe/pool/regenerate', {}); toast('已排队重建，约 2 秒生效'); setTimeout(loadPool, 2500); }
    catch (e) { toast(errText(e), 'bad'); }
  }
  async function clearOrders() {
    if (!(await confirmDialog({
      title: '清理并刷新全部订单',
      desc: '清空所有玩家当前的咖啡厅订单，并立刻重建候选池。玩家下次打开咖啡厅即为全新订单（约 2 秒生效）。此操作不可撤销。',
      confirm: '清理并刷新', danger: true,
    }))) return;
    try {
      await post('/api/cafe/orders/clear', {});
      toast('已提交清理指令，约 2 秒后生效');
      setTimeout(() => { loadPool(); render(); }, 2500);
    } catch (e) { toast(errText(e), 'bad'); }
  }
  await loadPool();

  return h('div', { class: 'page' },
    pageHead('咖啡厅订单', '悬赏赏金改为金币；按鱼种稀有度 / 奖杯 / 蓝冠 / 巨物分档定价。改完配置点底部「保存修改」，候选池会自动重建。',
      h('div', { class: 'row' }, btn('清理并刷新全部订单', clearOrders, 'danger sm', { icon: 'refresh' }), btn('重建候选池', regenPool, 'sm', { icon: 'refresh' }), btn('刷新', () => render(), 'ghost sm', { icon: 'refresh' }))),
    callout('info', h('span', {}, '金额单位：', h('b', {}, '金'), '（1 金 = 100 分）；轮换周期按小时填写。所有配置改动在页面底部统一保存。')),
    section({ title: '金币范围（六档）', desc: '按鱼种稀有度与奖杯 / 蓝冠分别定价。', body: bands }),
    section({ title: '巨物补贴', desc: '超过蓝冠线的巨物按重量级额外补贴。', body: subs }),
    section({ title: '交单数量', desc: '各档鱼每次交单需要的条数；奖杯位与长期位恒为 1 条。', body: qty }),
    section({ title: '轮换周期', desc: '长期位固定、奖杯位固定、普通 / 达标位逐槽在范围内轮换。', body: periods }),
    section({ title: '槽位占比', desc: '奖杯位 / 长期位 / 普通达标位的数量，合计必须等于 10。', body: mix }),
    section({ title: '按水域黑名单', desc: '选水域后搜索鱼种并点击加入；黑名单里的鱼不会进入该水域候选池。', body: blacklist }),
    section({ title: '候选池预览', desc: '按水域查看当前候选订单，最多显示 300 条。', actions: h('div', { class: 'row' }, poolLevel, poolCount), body: poolTable }));
}
