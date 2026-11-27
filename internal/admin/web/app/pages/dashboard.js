import { h, api, apiList, fmt, relTime, timeText, icon, navigate, section, badge, empty, pageHead, btn, render, setPageCleanup, debounce } from '../core.js';
import { auditLabel } from '../labels.js';
import { updateBanner } from '../main.js';

const SHORTCUTS = [
  ['players', 'users', '查找玩家', '改等级、货币、背包或技能'],
  ['mail', 'mail', '发邮件', '给玩家发消息或物品'],
  ['rules', 'rules', '调整玩法规则', '选择真实或随机的钓鱼体验'],
  ['waters', 'water', '设置水域鱼种', '每个水域出哪些鱼'],
  ['shop', 'store', '调整商店', '改商品价格和库存'],
];
const RULES = [
  ['fish_bait_rules_enabled', '鱼会挑饵'], ['fish_distribution_enabled', '鱼群分布'], ['fish_weather_enabled', '天气与水温'],
  ['fish_ecology_enabled', '鱼种习性'], ['bite_gate_enabled', '饵要对口'], ['happy_hour_enabled', '欢乐时光'],
];

export default async function dashboard() {
  const [d, audits, tuns, mode, stores] = await Promise.all([
    api('/api/dashboard'), apiList('/api/audit?limit=24').catch(() => []), api('/api/tunables').catch(() => ({})),
    api('/api/fish/bite_mode').catch(() => ({})), apiList('/api/stores').catch(() => []),
  ]);
  updateBanner(d.update);
  const timer = setInterval(() => { if (!document.hidden) render(); }, 8000);
  setPageCleanup(() => clearInterval(timer));
  const stats = [
    ['当前在线', d.sessions, '正在游戏的连接'],
    ['角色', d.players, '已创建的角色'],
    ['物品', d.items, '所有背包和装备里的物品'],
    ['后台修改', d.audits, '累计记录的操作'],
  ];
  const modeName = { realistic: '真实鱼汛', legacy: '全随机（旧版）', mixed: '自定义' }[mode.mode] || '未知';
  const ruleOn = (k) => (tuns[k] === undefined ? k === 'happy_hour_enabled' : tuns[k] === '1');

  // 当前生效的关键口径：只写能从接口读出来的东西（来自主分支，用我方 section 写法重写）。
  const canons = [];
  canons.push(['发鱼方式', mode.spawn_mode || modeName]);
  if (tuns.grade_pct_qualified !== undefined) {
    canons.push(['四档占比', `未达标 ${tuns.grade_pct_below || '0'}% · 合格 ${tuns.grade_pct_qualified}% · 奖杯 ${tuns.grade_pct_trophy || '0'}% · 蓝冠 ${tuns.grade_pct_super || '0'}%`]);
  }
  if (tuns.bite_delay_max !== undefined) {
    canons.push(['咬钩节奏', `普通上限 ${tuns.bite_delay_max} 秒${tuns.bite_delay_lure_max ? ' / 路亚上限 ' + tuns.bite_delay_lure_max + ' 秒' : ''}${String(tuns.bite_pace_enabled) === '1' ? '（饵效控速已开）' : ''}`]);
  }
  const enabled = RULES.filter(([k]) => ruleOn(k)).map(([, n]) => n);
  canons.push(['玩法开关', enabled.length ? enabled.join(' · ') : '全部关闭']);
  if (stores.length) {
    const overrideCount = stores.reduce((n, s) => n + (s.override_count || 0), 0);
    canons.push(['商店覆盖', overrideCount ? `${overrideCount} 件商品有覆盖价` : '没有覆盖，全部走基础价']);
  }
  canons.push(['饵要对口', ruleOn('bite_gate_enabled') ? '开：饵不对口不咬' : '关：只要饵能入口就咬']);

  // 最近的修改：按关键词筛动作/对象/管理员。
  const logState = { q: '' };
  const logBox = h('div', {});
  const drawLogs = () => {
    const q = logState.q.trim().toLowerCase();
    const rows = audits.filter((e) => {
      const s = `${auditLabel(e.Action ?? e.action)} ${e.Target ?? e.target} ${e.Admin ?? e.admin}`;
      return !q || s.toLowerCase().includes(q);
    });
    logBox.replaceChildren(rows.length ? h('div', { class: 'list' }, rows.map((e) => {
      const ts = e.TS ?? e.ts, target = (e.Target ?? e.target) || '';
      return h('div', { class: 'row', style: { padding: '9px 0', borderTop: '1px solid var(--line)', flexWrap: 'nowrap' } },
        h('span', { class: 'muted small nowrap', style: { width: '90px' }, title: timeText(ts), text: relTime(ts) }),
        h('b', { style: { fontWeight: '600' }, text: auditLabel(e.Action ?? e.action) }),
        h('span', { class: 'muted small grow', style: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }, text: target }),
        h('span', { class: 'muted small', text: e.Admin ?? e.admin ?? '' }));
    })) : empty('没有匹配的记录', '换个关键词，或点「刷新」重新拉一次审计表。'));
  };
  drawLogs();

  return h('div', { class: 'page' },
    pageHead('总览', '服务器现在的情况，以及常用功能的入口。', btn('刷新', () => render(), 'sm', { icon: 'refresh' })),
    h('div', { class: 'grid-4', style: { marginBottom: '16px' } },
      stats.map(([k, v, s]) => h('div', { class: 'card stat' }, h('div', { class: 'k', text: k }), h('div', { class: 'v', text: fmt(v) }), h('div', { class: 's', text: s })))),
    h('div', { class: 'grid-2', style: { gridTemplateColumns: 'minmax(0,1.4fr) minmax(0,1fr)', alignItems: 'start' } },
      section({
        title: '常用功能',
        body: h('div', { class: 'grid-2', style: { gap: '10px' } }, SHORTCUTS.map(([r, ic, t, d2]) => h('button', {
          class: 'card', type: 'button', onclick: () => navigate(r),
          style: { display: 'flex', gap: '12px', alignItems: 'center', padding: '14px', textAlign: 'left', background: 'var(--surface-2)', cursor: 'pointer' },
        }, h('span', { style: { width: '34px', height: '34px', borderRadius: '8px', display: 'grid', placeItems: 'center', background: 'var(--accent-soft)', color: 'var(--accent)', flex: 'none' } }, icon(ic, 17)),
        h('span', {}, h('b', { style: { display: 'block', fontWeight: 600 }, text: t }), h('span', { class: 'muted small', text: d2 }))))),
      }),
      section({
        title: '当前玩法',
        actions: btn('调整', () => navigate('rules'), 'sm'),
        body: h('div', {},
          h('div', { class: 'row', style: { marginBottom: '10px' } }, h('span', { class: 'muted small', text: '玩法风格' }), badge(modeName, 'accent')),
          h('div', { class: 'list' }, RULES.map(([k, n]) => h('div', { class: 'row', style: { justifyContent: 'space-between', padding: '7px 0', borderTop: '1px solid var(--line)' } },
            h('span', { text: n }), ruleOn(k) ? badge('开启', 'ok', true) : badge('关闭', '', true))))),
      })),
    section({
      title: '当前生效的关键口径', desc: '只列能从接口读出来的实时值，读不到就不显示。',
      body: h('div', { class: 'list' }, canons.map(([k, v]) => h('div', { class: 'row', style: { justifyContent: 'space-between', padding: '8px 0', borderTop: '1px solid var(--line)', flexWrap: 'nowrap', gap: '12px' } },
        h('b', { class: 'nowrap', style: { fontWeight: 600, minWidth: '84px' }, text: k }),
        h('span', { class: 'muted small grow', text: v })))),
    }),
    section({
      title: '最近的修改', desc: '后台审计表里最近的记录，可按动作、对象或管理员筛。',
      actions: h('div', { class: 'row', style: { gap: '6px' } },
        h('input', { class: 'input', type: 'search', placeholder: '搜动作、对象或管理员', style: { width: '210px' }, oninput: debounce((e) => { logState.q = e.target.value; drawLogs(); }, 200) }),
        btn('全部记录', () => navigate('audit'), 'sm')),
      body: logBox,
    }));
}
