// 天气调节：按地图设置温度随机区间、风速档位、六种天气概率。
import {
  h, api, toast, errText, pageHead, badge, btn, empty, loading, confirmDialog,
  navigate, Form, savebar, setLeaveGuard, render, tabs,
} from '../core.js';

// 与「水域与鱼种」同款：按官服译名顺序排列（不使用 key 排序）
const ORDER = ['克马羚诺也湖', '埃尔克湖', '惟有诺克河', '旧奥斯特罗格湖', '白河', '廓里湖', '梅德韦杰湖', '沃尔霍夫河', '北顿涅茨河', '苏拉河', '拉多加湖', '琥珀湖', '拉多加湖群岛', '阿赫图巴河', '铜湖', '下通古斯卡河', '亚马河', '挪威海'];
const WEATHERS = [
  ['晴', '#e0a743'], ['少云', '#9fb8c8'], ['多云', '#7f8b9a'],
  ['雨', '#4cb3d4'], ['暴雨', '#3a7fb5'], ['雷暴', '#9d7cf0'],
];
const WINDS = [['calm', '无风（0 m/s）'], ['normal', '正常（0~5 m/s）'], ['windy', '大风（5~12 m/s）']];
const DEFAULT_WEIGHTS = [3, 2, 1, 1, 1, 1];

export default async function weather(params) {
  const data = await api('/api/weather/maps');
  const maps = (data.items || []).slice().sort((a, b) => {
    const ia = ORDER.indexOf(a.name), ib = ORDER.indexOf(b.name);
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib);
  });
  const mapKey = params[0] && maps.some((m) => m.map_key === params[0]) ? params[0] : (maps[0] && maps[0].map_key);
  const tab = params[1] === 'probability' ? 'probability' : 'climate';

  const side = h('div', { class: 'card master' }, h('div', { class: 'list' }, maps.map((m) => {
    const on = m.map_key === mapKey;
    return h('div', { class: 'list-item clickable' + (on ? ' on' : ''), onclick: () => navigate('weather', m.map_key, tab) },
      h('div', { class: 'grow' }, h('div', { class: 'title', text: m.name }), h('div', { class: 'sub mono', text: m.map_key })),
      m.config && m.config.enabled ? badge('已配置', 'accent') : badge('默认'));
  })));
  const main = h('div', {}, h('div', { class: 'card card-pad' }, loading(6)));
  const root = h('div', { class: 'page' },
    pageHead('天气调节', '按地图调整温度随机区间、风速档位与阴晴雨概率；保存后约 2 秒内生效，在线玩家立即刷新。'),
    h('div', { class: 'master-detail', style: { gridTemplateColumns: '240px minmax(0,1fr)' } }, side, main));
  if (!mapKey) { main.replaceChildren(h('div', { class: 'card' }, empty('没有地图数据'))); return root; }

  const tabBar = tabs([['climate', '温度与风速'], ['probability', '天气概率']], tab, (t) => navigate('weather', mapKey, t));
  const m = maps.find((x) => x.map_key === mapKey);
  main.replaceChildren(h('div', { class: 'card card-pad' },
    h('h2', { style: { fontSize: '17px', marginBottom: '10px' }, text: m.name }), tabBar, weatherView(m, tab)));
  return root;
}

function weatherView(m, tab) {
  const c = m.config || {};
  const form = new Form();
  let weights = (c.kind_weights && c.kind_weights.length === 6) ? [...c.kind_weights] : [...DEFAULT_WEIGHTS];
  let dirtyWeights = false;
  savebar(form, {
    onSave: async () => {
      await api('/api/weather/control', {
        method: 'PUT',
        body: JSON.stringify({
          map_key: m.map_key,
          temp_base: Number(form.value('temp_base')),
          temp_swing: Number(form.value('temp_swing')),
          wind_mode: form.value('wind_mode'),
          kind_weights: [...weights],
          enabled: true,
        }),
      });
      dirtyWeights = false;
      toast('已保存，约 2 秒后在线玩家天气刷新');
      setTimeout(render, 200);
    },
  });
  // 天气权重表格不走 Form 字段，手动算进「未保存」（与鱼种权重表同款）
  const origDirty = form.dirtyKeys.bind(form);
  form.dirtyKeys = () => { const k = origDirty(); return dirtyWeights && !k.length ? ['_weights'] : k; };
  const origValues = form.values.bind(form);
  form.values = (keys) => origValues((keys || []).filter((k) => k !== '_weights'));
  const origReset = form.reset.bind(form);
  form.reset = () => { dirtyWeights = false; weights = (c.kind_weights && c.kind_weights.length === 6) ? [...c.kind_weights] : [...DEFAULT_WEIGHTS]; origReset(); render(); };
  setLeaveGuard(() => form.isDirty());

  const climate = h('div', { class: 'stack' },
    form.row({ key: 'temp_base', label: '温度基准线', help: '0 表示沿用地图模板气候；设为整数 °C，如 22。', kind: 'int', min: -50, max: 60, unit: '°C', def: '0' }, c.temp_base ?? 0),
    form.row({ key: 'temp_swing', label: '温度上下浮动', help: '每天最高温在基准 ±浮动 内整数随机；0 表示沿用模板区间。', kind: 'int', min: 0, max: 40, unit: '°C', def: '0' }, c.temp_swing ?? 0),
    form.row({ key: 'wind_mode', label: '风速档位', help: '5 m/s 是大风与正常的界线；无风=0，正常=0~5，大风=5~12。', kind: 'select', options: WINDS, def: 'normal' }, c.wind_mode || 'normal'),
  );

  const tbody = h('tbody');
  const total = () => weights.reduce((a, b) => a + b, 0) || 1;
  const draw = () => {
    const sum = total();
    tbody.replaceChildren(...WEATHERS.map(([name, color], i) => {
      const w = h('input', { class: 'input num', style: { width: '90px', height: '30px' }, onchange: (e) => {
        const raw = e.target.value, n = Number(raw);
        if (raw.trim() !== '' && (!Number.isFinite(n) || n < 0)) { toast('权重必须为非负数，已保留原值', 'bad'); e.target.value = weights[i]; return; }
        weights[i] = raw.trim() !== '' && Number.isFinite(n) ? n : DEFAULT_WEIGHTS[i];
        dirtyWeights = true; form.refresh(); draw();
      } });
      w.value = weights[i];
      const pct = weights[i] / sum * 100;
      return h('tr', {},
        h('td', {}, h('div', { class: 'row', style: { flexWrap: 'nowrap' } },
          h('span', { style: { width: '10px', height: '10px', borderRadius: '50%', background: color, flex: 'none' } }),
          h('b', { text: name }))),
        h('td', {}, w, h('span', { class: 'sub', text: '默认 ' + DEFAULT_WEIGHTS[i] })),
        h('td', { style: { width: '240px' } }, h('div', { class: 'row', style: { flexWrap: 'nowrap' } },
          h('div', { style: { flex: 1, height: '6px', borderRadius: '3px', background: 'var(--surface-4)', overflow: 'hidden' } },
            h('div', { style: { width: Math.min(100, pct * 3) + '%', height: '100%', background: color } })),
          h('span', { class: 'small', style: { width: '52px', textAlign: 'right' }, text: pct.toFixed(1) + '%' }))));
    }));
  };
  draw();
  const probability = h('div', { class: 'stack' },
    h('p', { class: 'muted small', text: '六种天气按权重随机；权重是相对值，保存后自动归一化并重新生成 64 天序列。调高某项即更容易刷出该天气。' }),
    h('div', { class: 'table-wrap' }, h('table', { class: 'table' },
      h('thead', {}, h('tr', {}, h('th', { text: '天气' }), h('th', { text: '权重' }), h('th', { text: '概率' }))), tbody)));

  const toolbar = h('div', { class: 'row' },
    h('span', { class: 'grow' }),
    btn('恢复默认', async () => {
      if (!(await confirmDialog({ title: `恢复「${m.name}」默认天气？`, desc: '删除该图覆盖配置，恢复官服默认天气并刷新在线玩家。', confirm: '恢复默认', danger: true }))) return;
      try {
        await api('/api/weather/control?map=' + encodeURIComponent(m.map_key), { method: 'DELETE' });
        toast('已恢复默认');
        render();
      } catch (e) { toast(errText(e), 'bad'); }
    }, 'ghost sm'));

  return h('div', { class: 'stack' },
    tab === 'climate' ? climate : probability,
    toolbar,
    h('p', { class: 'muted small', text: '保存后服务端重算天气序列，并把新地图数据推给在线玩家；若客户端未立即变化，重新进图即可。' }));
}
