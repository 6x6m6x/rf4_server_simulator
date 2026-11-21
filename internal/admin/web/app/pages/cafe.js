// 咖啡厅订单：悬赏赏金改金币，按鱼种稀有度 / 奖杯 / 蓝冠 / 巨物分档定价。
// 改配置后后端会自动重建候选池；本页负责配置编辑、黑名单与候选池预览。
import {
  h, api, post, toast, errText, pageHead, section, btn, confirmDialog,
  searchBox, debounce, render,
} from '../core.js';

const BAND_LABEL = {
  normal_common: '普通/达标·常见', normal_rare: '普通/达标·稀有',
  trophy_common: '奖杯·常见', trophy_rare: '奖杯·稀有',
  blue_common: '蓝冠·常见', blue_rare: '蓝冠·稀有',
};
const QTY_LABEL = {
  common_normal: '普通鱼', common_qualified: '达标鱼',
  rare_normal: '稀有普通鱼', rare_qualified: '稀有达标鱼',
};
const PERIOD_LABEL = { longterm: '长期位', trophy: '奖杯位', normal: '普通/达标位' };
const MIX_LABELS = [['trophy', '奖杯位'], ['longterm', '长期位'], ['normal', '普通/达标位']];

// 金额界面单位：金；后端单位：分（100 分 = 1 金）
const toCents = (v) => Math.round(parseFloat(v) * 100);
const toGold = (cents) => String(Number(cents || 0) / 100);

export default async function cafe() {
  let cfg = null;
  let levels = [];
  let fishHits = [];
  let mixInputs = {};

  // ---------- 容器：局部持有 DOM 引用，重绘只替换内部节点 ----------
  const bandsBox = h('div');
  const subsBox = h('div');
  const qtyBox = h('div');
  const periodBox = h('div');
  const mixBox = h('div', { class: 'grid-3' });
  const mixHint = h('div', { class: 'small muted', style: { marginTop: '8px' } });

  const blLevel = h('select', { class: 'select', 'aria-label': '选择水域', onchange: () => renderBlacklist() });
  const blSearchWrap = searchBox('输入鱼种中文名或 key 搜索', (v) => runSearch(v));
  const blSearchInput = blSearchWrap.querySelector('input');
  const blHits = h('div', { class: 'row', style: { flexWrap: 'wrap', marginTop: '10px', minHeight: '20px' } });
  const blTableBox = h('div');

  const poolLevel = h('select', { class: 'select', style: { width: '200px' }, 'aria-label': '筛选水域', onchange: () => { loadPool(); } });
  const poolSummary = h('span', { class: 'muted small', text: '0 条候选' });
  const poolTableBox = h('div', { class: 'table-wrap', style: { maxHeight: 'calc(100vh - 420px)' } });

  // ---------- 数据加载 ----------
  async function load() {
    try {
      cfg = await api('/api/cafe/config');
      const lv = await api('/api/cafe/pool');
      levels = lv.levels || [];
    } catch (e) { toast(errText(e), 'bad'); return; }
    renderBands();
    renderSubsidies();
    renderQuantities();
    renderPeriods();
    renderSlotMix();
    fillLevels(blLevel, false);
    fillLevels(poolLevel, true);
    renderBlacklist();
    await loadPool();
  }

  // 重建水域下拉，并尽量保留当前选中项
  function fillLevels(sel, withAll) {
    const cur = sel.value;
    const opts = [];
    if (withAll) opts.push(h('option', { value: '', text: '全部水域' }));
    if (levels.length) for (const l of levels) opts.push(h('option', { value: l, text: l }));
    else if (!withAll) opts.push(h('option', { value: '', text: '（无水域）' }));
    sel.replaceChildren(...opts);
    if ([...sel.options].some((o) => o.value === cur)) sel.value = cur;
    else sel.value = withAll ? '' : (levels[0] || '');
  }

  function numInput(value, extra = {}) {
    return h('input', { type: 'number', class: 'input num', style: { width: '96px', height: '30px' }, value: String(value), ...extra });
  }

  function table(head, body) {
    return h('div', { class: 'table-wrap' }, h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, ...head.map((t) => h('th', { text: t })))),
      h('tbody', {}, ...body)));
  }

  // ---------- 金币范围六档 ----------
  function renderBands() {
    const bands = (cfg && cfg.bands) || [];
    const rows = bands.map((b) => {
      const lo = numInput(toGold(b.lo_cents), { step: '0.5', min: '0' });
      const hi = numInput(toGold(b.hi_cents), { step: '0.5', min: '0' });
      const save = btn('保存', async () => {
        const loC = toCents(lo.value), hiC = toCents(hi.value);
        if (!Number.isFinite(loC) || !Number.isFinite(hiC) || loC < 0 || hiC < loC) { toast('金额范围无效', 'bad'); return; }
        try {
          await post('/api/cafe/band', { key: b.key, lo_cents: loC, hi_cents: hiC });
          toast('已保存并排队重建候选池');
          await load();
        } catch (e) { toast(errText(e), 'bad'); }
      }, 'primary sm');
      return h('tr', {}, h('td', { text: BAND_LABEL[b.key] || b.label || b.key }), h('td', {}, lo), h('td', {}, hi), h('td', {}, save));
    });
    bandsBox.replaceChildren(table(['档位', '下限(金)', '上限(金)', ''], rows));
  }

  // ---------- 巨物补贴 ----------
  function renderSubsidies() {
    const subs = (cfg && cfg.subsidies) || [];
    const rows = subs.map((s) => {
      const min = numInput(s.min_blue_g);
      const lo = numInput(toGold(s.lo_cents), { step: '0.5' });
      const hi = numInput(toGold(s.hi_cents), { step: '0.5' });
      const save = btn('保存', async () => {
        const minG = Math.round(parseFloat(min.value) || 0);
        const loC = toCents(lo.value), hiC = toCents(hi.value);
        if (!Number.isFinite(loC) || !Number.isFinite(hiC) || loC < 0 || hiC < loC) { toast('补贴范围无效', 'bad'); return; }
        try {
          await post('/api/cafe/subsidy', { grp: s.grp, min_blue_g: minG, lo_cents: loC, hi_cents: hiC });
          toast('已保存并排队重建候选池');
          await load();
        } catch (e) { toast(errText(e), 'bad'); }
      }, 'primary sm');
      return h('tr', {},
        h('td', { text: String(s.label || s.grp).split('（')[0] }),
        h('td', {}, min), h('td', {}, lo), h('td', {}, hi), h('td', {}, save));
    });
    subsBox.replaceChildren(table(['重量级', '蓝冠线起(g)', '下限(金)', '上限(金)', ''], rows));
  }

  // ---------- 交单数量 ----------
  function renderQuantities() {
    const items = (cfg && cfg.quantities) || [];
    const rows = items.map((q) => {
      const input = numInput(q.quantity, { min: '1', max: '1000' });
      const save = btn('保存', async () => {
        const quantity = parseInt(input.value, 10);
        if (!Number.isFinite(quantity) || quantity < 1 || quantity > 1000) { toast('数量须为 1~1000', 'bad'); return; }
        try {
          await post('/api/cafe/quantity', { qkey: q.qkey, quantity });
          toast('已保存并排队重建候选池');
          await load();
        } catch (e) { toast(errText(e), 'bad'); }
      }, 'primary sm');
      return h('tr', {}, h('td', { text: QTY_LABEL[q.qkey] || q.label || q.qkey }), h('td', {}, input), h('td', {}, save));
    });
    qtyBox.replaceChildren(table(['档位', '需交(条)', ''], rows));
  }

  // ---------- 轮换周期（界面按小时，后端存秒） ----------
  function renderPeriods() {
    const items = (cfg && cfg.periods) || [];
    const rows = items.map((p) => {
      const min = numInput(Number(p.min_seconds) / 3600, { step: '0.5', min: '0.01' });
      const max = numInput(Number(p.max_seconds) / 3600, { step: '0.5', min: '0.01' });
      const save = btn('保存', async () => {
        const minS = Math.round(parseFloat(min.value) * 3600);
        const maxS = Math.round(parseFloat(max.value) * 3600);
        if (!Number.isFinite(minS) || !Number.isFinite(maxS) || minS <= 0 || maxS < minS) { toast('周期范围无效', 'bad'); return; }
        try {
          await post('/api/cafe/period', { kind: p.kind, min_seconds: minS, max_seconds: maxS });
          toast('已保存并排队重建候选池');
          await load();
        } catch (e) { toast(errText(e), 'bad'); }
      }, 'primary sm');
      return h('tr', {}, h('td', { text: PERIOD_LABEL[p.kind] || p.kind }), h('td', {}, min), h('td', {}, max), h('td', {}, save));
    });
    periodBox.replaceChildren(table(['槽位', '最短(时)', '最长(时)', ''], rows));
  }

  // ---------- 槽位占比 ----------
  function updateMixHint() {
    const total = Object.values(mixInputs).reduce((a, i) => a + (Number(i.value) || 0), 0);
    mixHint.textContent = '当前合计 ' + total + ' / 10' + (total === 10 ? ' ✓' : '（必须为 10）');
    mixHint.style.color = total === 10 ? '' : '#ef4444';
  }
  function renderSlotMix() {
    const mix = (cfg && cfg.slot_mix) || [];
    mixInputs = {};
    const fields = MIX_LABELS.map(([kind, label]) => {
      const row = mix.find((m) => m.kind === kind);
      const input = numInput(row ? row.count : 0, { min: '0', max: '10', oninput: updateMixHint });
      mixInputs[kind] = input;
      return h('div', { class: 'field' }, h('label', { text: label }), input);
    });
    mixBox.replaceChildren(...fields);
    updateMixHint();
  }
  async function saveSlotMix() {
    const counts = {};
    for (const [kind] of MIX_LABELS) counts[kind] = Number(mixInputs[kind] && mixInputs[kind].value) || 0;
    if (counts.trophy + counts.longterm + counts.normal !== 10) { toast('三类数量总和必须为 10', 'bad'); return; }
    try {
      await post('/api/cafe/slot-mix', counts);
      toast('已保存并排队重建候选池');
      await load();
    } catch (e) { toast(errText(e), 'bad'); }
  }

  // ---------- 按水域黑名单 ----------
  const runSearch = debounce(async (v) => {
    const q = v.trim();
    if (!q) { fishHits = []; blHits.replaceChildren(); return; }
    try {
      const d = await api('/api/cafe/fish/search?q=' + encodeURIComponent(q));
      fishHits = d.items || [];
      blHits.replaceChildren(...(fishHits.length
        ? fishHits.map((f, i) => h('span', {
          class: 'pill alt',
          style: { display: 'inline-block', margin: '2px 4px 2px 0', padding: '3px 9px', border: '1px solid var(--line,#ccc)', borderRadius: '12px', cursor: 'pointer' },
          onclick: () => addBlacklist(i),
        }, f.name_zh + ' ', h('span', { class: 'muted small', text: f.fish_key })))
        : [h('span', { class: 'muted small', text: '无匹配鱼种' })]));
    } catch (e) {
      blHits.replaceChildren(h('span', { class: 'muted small', text: '搜索失败：' + errText(e) }));
    }
  }, 180);

  async function addBlacklist(idx) {
    const f = fishHits[idx];
    const level = blLevel.value;
    if (!f || !level) { toast('请先选择水域', 'bad'); return; }
    try {
      await post('/api/cafe/blacklist/add', { level, fish_key: f.fish_key, name_zh: f.name_zh });
      toast('已加入黑名单并排队重建候选池');
      blSearchInput.value = '';
      blHits.replaceChildren();
      fishHits = [];
      await load();
    } catch (e) { toast(errText(e), 'bad'); }
  }

  async function removeBlacklist(key) {
    const level = blLevel.value;
    if (!(await confirmDialog({ title: '移除黑名单', desc: '从 ' + level + ' 移除黑名单 ' + key + '？', confirm: '移除', danger: true }))) return;
    try {
      await post('/api/cafe/blacklist/remove', { level, fish_key: key });
      toast('已移除');
      await load();
    } catch (e) { toast(errText(e), 'bad'); }
  }

  function renderBlacklist() {
    const level = blLevel.value;
    const all = (cfg && cfg.blacklist) || [];
    const rows = all.filter((b) => b.level === level);
    const body = rows.length
      ? rows.map((b) => h('tr', {}, h('td', { text: b.level }), h('td', { text: b.fish_key }), h('td', { text: b.name_zh }),
        h('td', {}, btn('移除', () => removeBlacklist(b.fish_key), 'alt sm'))))
      : [h('tr', {}, h('td', { colspan: '4', class: 'muted', text: level ? '该水域暂无黑名单' : '(请先选择水域)' }))];
    blTableBox.replaceChildren(table(['水域', '鱼种 key', '中文名', ''], body));
  }

  // ---------- 候选池预览 ----------
  async function loadPool() {
    const lv = poolLevel.value;
    try {
      const data = await api('/api/cafe/pool' + (lv ? '?level=' + encodeURIComponent(lv) : ''));
      const rows = data.rows || [];
      poolSummary.textContent = rows.length + ' 条候选';
      const body = rows.slice(0, 300).map((r) => h('tr', {},
        h('td', { text: r.level }), h('td', { text: r.slot }),
        h('td', {}, r.name_zh + ' ', h('span', { class: 'muted small', text: r.species })),
        h('td', { text: r.grade }), h('td', { text: String(r.weight_g) }), h('td', { text: String(r.quantity || 1) }),
        h('td', { text: r.is_rare ? '是' : '' }), h('td', { text: r.is_giant ? '是' : '' }),
        h('td', { text: (Number(r.subsidy_cents) / 100).toFixed(1) }),
        h('td', {}, h('b', { text: (Number(r.gold_cents) / 100).toFixed(2) }))));
      poolTableBox.replaceChildren(table(
        ['水域', '槽位', '鱼种', '档位', '重量(g)', '需交(条)', '稀有', '巨物', '补贴(金)', '悬赏(金)'], body));
      if (rows.length > 300) poolTableBox.append(h('p', { class: 'muted small', text: '仅显示前 300 条，共 ' + rows.length + ' 条' }));
    } catch (e) { toast(errText(e), 'bad'); }
  }

  async function regenPool() {
    if (!(await confirmDialog({
      title: '重建候选池',
      desc: '重建全部水域候选池？当前在线订单在下个周期切换后使用新池。',
      confirm: '重建', danger: true,
    }))) return;
    try {
      await post('/api/cafe/pool/regenerate', {});
      toast('已排队重建，约 2 秒内生效');
      setTimeout(loadPool, 2500);
    } catch (e) { toast(errText(e), 'bad'); }
  }

  // ---------- 页面骨架 ----------
  const root = h('div', { class: 'page' },
    pageHead('咖啡厅订单', '悬赏赏金改金币；按鱼种稀有度/奖杯/蓝冠/巨物分档定价；改配置后自动重建候选池。',
      btn('刷新', () => render(), 'ghost sm', { icon: 'refresh' })),
    section({ title: '金币范围（六档）', desc: '按鱼种与奖杯 / 蓝冠分档定价。单位：金，1 金 = 100 分。', body: bandsBox }),
    section({ title: '巨物补贴', desc: '蓝冠线以上的巨物额外补贴，按重量级分档。单位：金。', body: subsBox }),
    section({ title: '交单数量', desc: '各档鱼每次交单需要的条数。', body: qtyBox }),
    section({ title: '轮换周期', desc: '界面按小时填写，保存时换算成秒存入后端。', body: periodBox }),
    section({
      title: '槽位占比', desc: '奖杯位 / 长期位 / 普通达标位的数量，三类总和必须等于 10。',
      actions: btn('保存占比', saveSlotMix, 'primary sm'),
      body: h('div', {}, mixBox, mixHint),
    }),
    section({
      title: '按水域黑名单', desc: '选水域后搜索鱼种并点击加入；黑名单中的鱼不会进入该水域候选池。',
      body: h('div', { class: 'stack' },
        h('div', { class: 'row' }, blLevel, h('div', { class: 'grow', style: { maxWidth: '360px' } }, blSearchWrap)),
        blHits, blTableBox),
    }),
    section({
      title: '候选池预览', desc: '按水域查看当前候选订单，最多显示 300 条。',
      actions: h('div', { class: 'row' }, poolLevel, btn('重建候选池', regenPool, 'primary sm', { icon: 'refresh' })),
      body: h('div', { class: 'stack' }, poolSummary, poolTableBox),
    }));

  await load();
  return root;
}
