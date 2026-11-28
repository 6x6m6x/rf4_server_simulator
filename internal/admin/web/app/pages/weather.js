// 天气调节：按地图设置温度随机区间、风速档位、六种天气概率。
import { h, api, post, toast, errText, pageHead, section, callout, badge, btn, segmented, confirmDialog } from '../core.js';

const WEATHERS = [
  ['晴', '#e0a743'], ['少云', '#9fb8c8'], ['多云', '#7f8b9a'],
  ['雨', '#4cb3d4'], ['暴雨', '#3a7fb5'], ['雷暴', '#9d7cf0'],
];
const WINDS = [
  ['calm', '无风 0'], ['normal', '正常 0~5'], ['windy', '大风 5~12'],
];
const DEFAULT_WEIGHTS = [3, 2, 1, 1, 1, 1];

const clampInt = (v, lo, hi) => Math.max(lo, Math.min(hi, Math.round(Number(v) || 0)));

export default async function weather() {
  const root = h('div', { class: 'page' });
  root.append(pageHead('天气调节', '按地图调整温度随机区间、风速档位与阴晴雨概率；保存后约 2 秒内生效，在线玩家立即刷新。'));

  const state = { maps: [], selected: null, dirty: false };
  const listBox = h('div', { class: 'list' });
  const editBox = h('div', {});
  const foot = h('div', { class: 'row', style: { gap: '10px', marginTop: '18px' } });

  const askReload = async () => {
    if (!(await confirmDialog({ title: '保存并立即刷新天气？', desc: '配置写入后会自动重算天气序列并推送给在线玩家，约 2 秒生效。', confirm: '保存并刷新' }))) return false;
    return true;
  };

  function slider(label, help, value, min, max, unit, onInput) {
    const out = h('span', { class: 'mono', style: { minWidth: '56px', textAlign: 'right' }, text: `${value}${unit}` });
    const input = h('input', {
      type: 'range', min: String(min), max: String(max), step: '1', value: String(value),
      style: { flex: '1', accentColor: 'var(--accent)' },
      oninput: () => { const v = clampInt(input.value, min, max); out.textContent = `${v}${unit}`; onInput(v); },
    });
    return h('div', { class: 'setting' },
      h('div', {}, h('div', { class: 's-label', text: label }), h('div', { class: 's-help', text: help })),
      h('div', { class: 'row', style: { gap: '10px', alignItems: 'center', minWidth: '260px' } }, input, out));
  }

  function weatherBars(getWeights, onChange) {
    const colors = h('div', { style: { display: 'flex', height: '12px', borderRadius: '6px', overflow: 'hidden', background: 'var(--surface-4)' } });
    const num = h('span', { class: 'muted small' });
    const rows = WEATHERS.map(([name, color], i) => {
      const label = h('span', { class: 'small', style: { minWidth: '42px' }, text: name });
      const val = h('span', { class: 'mono small', style: { minWidth: '28px', textAlign: 'right' } });
      const range = h('input', {
        type: 'range', min: '0', max: '100', step: '1', value: String(getWeights()[i]),
        style: { flex: '1', accentColor: color },
        oninput: () => { const w = getWeights(); w[i] = clampInt(range.value, 0, 100); val.textContent = String(w[i]); draw(); onChange(); },
      });
      val.textContent = String(getWeights()[i]);
      return { row: h('div', { class: 'row', style: { gap: '10px', alignItems: 'center' } }, label, range, val), color, i };
    });
    const draw = () => {
      const w = getWeights();
      const total = w.reduce((a, b) => a + b, 0) || 1;
      colors.replaceChildren(...w.map((v, i) => h('div', {
        style: { width: (v / total * 100) + '%', background: WEATHERS[i][1], height: '100%' },
        title: `${WEATHERS[i][0]} ${(v / total * 100).toFixed(1)}%`,
      })));
      num.textContent = '概率 ' + w.map((v) => (v / total * 100).toFixed(0) + '%').join(' / ');
    };
    draw();
    return h('div', { class: 'stack', style: { gap: '8px' } },
      colors, h('div', { class: 'row', style: { justifyContent: 'space-between' } }, h('span', { class: 'muted small', text: '权重（相对值，自动归一化）' }), num),
      ...rows.map((r) => r.row));
  }

  function renderList() {
    listBox.replaceChildren(...state.maps.map((m) => {
      const on = state.selected === m.map_key;
      const configured = !!m.config && m.config.enabled;
      return h('button', {
        type: 'button',
        class: 'list-item clickable' + (on ? ' on' : ''),
        style: on ? { borderColor: 'var(--accent)', background: 'var(--accent-soft)' } : {},
        onclick: () => { state.selected = m.map_key; renderList(); renderEdit(); },
      },
        h('div', { class: 'row', style: { justifyContent: 'space-between', width: '100%' } },
          h('span', { text: m.name }),
          configured ? badge('已配置', 'accent') : badge('默认', '')),
        h('div', { class: 'muted small mono', text: m.map_key }));
    }));
  }

  function renderEdit() {
    const m = state.maps.find((x) => x.map_key === state.selected);
    if (!m) { editBox.replaceChildren(h('div', { class: 'muted', text: '请从左侧选择地图。' })); return; }
    const c = m.config || {};
    const draft = {
      tempBase: clampInt(c.temp_base ?? 0, -50, 60),
      tempSwing: clampInt(c.temp_swing ?? 0, 0, 40),
      wind: c.wind_mode || 'normal',
      weights: (c.kind_weights && c.kind_weights.length === 6) ? [...c.kind_weights] : [...DEFAULT_WEIGHTS],
      enabled: c.enabled !== false,
    };
    const mark = () => { state.dirty = true; };
    const windSeg = segmented(WINDS, draft.wind, (v) => { draft.wind = v; mark(); });
    const enableSeg = segmented([['1', '启用'], ['0', '关闭（恢复默认）']], draft.enabled ? '1' : '0', (v) => { draft.enabled = v === '1'; mark(); });
    const weightsBox = weatherBars(() => draft.weights, mark);
    editBox.replaceChildren(
      h('div', { class: 'row', style: { justifyContent: 'space-between', alignItems: 'center' } },
        h('div', {}, h('h2', { style: { margin: '0 0 4px' }, text: m.name }), h('div', { class: 'muted small mono', text: m.map_key })),
        badge(m.map_key.includes('carp') || m.map_key.includes('grass') || m.map_key.includes('moskit') ? '鲤钓图' : '普通水域', 'accent')),
      h('div', { class: 'stack', style: { gap: '4px', marginTop: '18px' } },
        slider('温度基准线', '0 表示沿用地图模板气候；设为整数 °C，如 22。', draft.tempBase, -50, 60, '°C', (v) => { draft.tempBase = v; mark(); }),
        slider('温度上下浮动', '每天最高温在基准 ±浮动 内整数随机；0 表示沿用模板区间。', draft.tempSwing, 0, 40, '°C', (v) => { draft.tempSwing = v; mark(); }),
        h('div', { class: 'setting' },
          h('div', {}, h('div', { class: 's-label', text: '风速档位' }), h('div', { class: 's-help', text: '5 m/s 为大风与正常的界线；无风=0，正常=0~5，大风=5~12。' })), windSeg),
        h('div', { class: 'setting' },
          h('div', {}, h('div', { class: 's-label', text: '天气概率' }), h('div', { class: 's-help', text: '六种天气按权重随机，保存后重算序列；调高某项即更容易刷出该天气。' })), weightsBox),
        h('div', { class: 'setting' },
          h('div', {}, h('div', { class: 's-label', text: '状态' }), h('div', { class: 's-help', text: '关闭后该图删除覆盖，恢复官服默认天气。' })), enableSeg)),
      foot,
    );
    foot.replaceChildren(
      btn('保存并刷新', async () => {
        if (!(await askReload())) return;
        try {
          await api('/api/weather/control', {
            method: 'PUT',
            body: JSON.stringify({
              map_key: m.map_key, temp_base: draft.tempBase, temp_swing: draft.tempSwing,
              wind_mode: draft.wind, kind_weights: draft.weights, enabled: draft.enabled,
            }),
          });
          toast('已保存，约 2 秒后在线玩家天气刷新');
          state.dirty = false;
          await load();
        } catch (e) { toast(errText(e), 'bad'); }
      }, 'primary sm'),
      m.config ? btn('恢复默认', async () => {
        if (!(await confirmDialog({ title: `恢复「${m.name}」默认天气？`, desc: '删除该图覆盖配置，恢复官服默认天气并刷新在线玩家。', confirm: '恢复默认', danger: true }))) return;
        try {
          await api('/api/weather/control?map=' + encodeURIComponent(m.map_key), { method: 'DELETE' });
          toast('已恢复默认');
          state.dirty = false;
          await load();
        } catch (e) { toast(errText(e), 'bad'); }
      }, 'sm') : null,
    );
  }

  async function load() {
    const data = await api('/api/weather/maps');
    state.maps = (data.items || []).slice().sort((a, b) => a.map_key.localeCompare(b.map_key));
    if (!state.maps.some((x) => x.map_key === state.selected)) state.selected = state.maps[0]?.map_key || null;
    renderList(); renderEdit();
  }

  root.append(
    section({ title: '天气与温度', desc: '所有 18 张地图均可单独配置；温度取整数随机，风速按档位，阴晴雨按权重。' },
      callout('info', '保存后服务端会重算天气序列并把新地图数据推给在线玩家；若客户端未立即变化，重新进图即可看到。')),
    h('div', { class: 'master-detail', style: { marginTop: '16px' } },
      h('div', { class: 'card card-pad', style: { minWidth: '240px' } }, listBox),
      h('div', { class: 'card card-pad', style: { flex: '1' } }, editBox)),
  );

  try { await load(); }
  catch (e) { editBox.replaceChildren(callout('bad', '天气配置加载失败：' + errText(e))); }
  return root;
}
