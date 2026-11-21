// Command cafe-bounty-report 导出各咖啡厅悬赏的可视化 HTML 快照。
// 数据来自真实的咖啡厅订单生成逻辑，数值与服务端运行时一致。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"rf4server/internal/config"
	"rf4server/internal/gameserver"
	"rf4server/internal/metrics"
	"rf4server/internal/store"
)

func main() {
	dbFlag := flag.String("db", "data/rf4server.db", "运行库路径")
	staticFlag := flag.String("static", "server_data/static.sqlite3", "静态库路径")
	outFlag := flag.String("out", "cafe_bounty_report.html", "输出 HTML 路径")
	flag.Parse()

	db, err := store.OpenWithStatic(*dbFlag, *staticFlag)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		log.Fatalf("迁移失败: %v", err)
	}

	srv := gameserver.NewServer(config.Game{}, slog.New(slog.NewTextHandler(io.Discard, nil)), (*metrics.Server)(nil))
	srv.SetDB(db)

	ctx := context.Background()
	levels, err := db.CafePoolLevels(ctx)
	if err != nil || len(levels) == 0 {
		// 池未生成：先迁移后重建，确保报告反映真实候选池。
		if _, err := srv.RegenerateCafeBountyPool(ctx); err != nil {
			log.Fatalf("生成候选池失败: %v", err)
		}
		levels, err = db.CafePoolLevels(ctx)
	}
	if err != nil || len(levels) == 0 {
		levels = gameserver.CafeWaterLevels() // 极端回退旧水域列表
	}
	groups, err := gameserver.BuildCafeBountyReport(ctx, srv, levels, time.Now().Unix())
	if err != nil {
		log.Fatalf("生成悬赏报告失败: %v", err)
	}

	payload, err := json.Marshal(groups)
	if err != nil {
		log.Fatal(err)
	}
	data := reportData{JSON: template.JS(payload), Generated: time.Now().Format("2006-01-02 15:04:05"), WaterCount: len(groups)}

	out, err := os.Create(*outFlag)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := reportTmpl.Execute(out, data); err != nil {
		log.Fatal(err)
	}
	abs, _ := filepath.Abs(*outFlag)
	fmt.Printf("已导出 %d 个咖啡厅悬赏报告: %s\n", len(groups), abs)
}

type reportData struct {
	JSON        template.JS
	Generated   string
	WaterCount  int
}

var reportTmpl = template.Must(template.New("report").Parse(pageHTML))

const pageHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>咖啡厅任务悬赏 · 金币梯度</title>
<style>
:root{--bg:#f5f7fa;--card:#fff;--ink:#1f2937;--muted:#6b7280;--line:#e5e7eb;--common:#0ea5e9;--rarecommon:#0284c7;--trophy:#f59e0b;--raretrophy:#d97706;--blue:#6366f1;--rareblue:#7c3aed;--cbg:#eef9ff;--rcbg:#e0f2fe;--tbg:#fff8e8;--rtbg:#fef3c7;--bbg:#eef2ff;--rbbg:#f3e8ff}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.5 -apple-system,"Segoe UI","Microsoft YaHei",sans-serif}
header{background:linear-gradient(135deg,#0ea5e9,#6366f1);color:#fff;padding:24px 32px}
header h1{margin:0 0 6px;font-size:24px}
header p{margin:2px 0;opacity:.92;font-size:13px}
.wrap{max-width:1240px;margin:0 auto;padding:20px 24px 60px}
.legend{display:flex;gap:14px;flex-wrap:wrap;margin:8px 0 20px}
.legend span{display:inline-flex;align-items:center;gap:6px;background:#fff;border:1px solid var(--line);padding:6px 12px;border-radius:20px;font-size:13px}
.dot{width:10px;height:10px;border-radius:50%;display:inline-block}
.rules{background:#fff;border:1px solid var(--line);border-radius:12px;padding:14px 18px;margin-bottom:22px;color:var(--muted);font-size:13px}
.rules b{color:var(--ink)}
.rules code{background:#f1f5f9;padding:1px 6px;border-radius:4px;color:#0f172a}
.water{background:var(--card);border:1px solid var(--line);border-radius:14px;margin-bottom:18px;overflow:hidden;box-shadow:0 1px 3px rgba(0,0,0,.04)}
.water h2{margin:0;padding:14px 18px;font-size:16px;background:#fafbfc;border-bottom:1px solid var(--line);display:flex;justify-content:space-between;align-items:center}
.water h2 small{font-weight:400;color:var(--muted);font-size:12px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:8px 12px;text-align:left;border-bottom:1px solid var(--line);white-space:nowrap}
th{background:#fafbfc;color:var(--muted);font-weight:600;font-size:12px}
tr:last-child td{border-bottom:none}
.tier-common td{background:var(--cbg)}
.tier-rarecommon td{background:var(--rcbg)}
.tier-trophy td{background:var(--tbg)}
.tier-raretrophy td{background:var(--rtbg)}
.tier-blue td{background:var(--bbg)}
.tier-rareblue td{background:var(--rbbg)}
.gold{font-weight:700;font-variant-numeric:tabular-nums}
.t-common .gold{color:var(--common)}.t-rarecommon .gold{color:var(--rarecommon)}.t-trophy .gold{color:var(--raretrophy)}.t-raretrophy .gold{color:var(--raretrophy)}.t-blue .gold{color:var(--blue)}.t-rareblue .gold{color:var(--rareblue)}
.pill{display:inline-block;padding:2px 9px;border-radius:12px;font-size:11px;font-weight:600;color:#fff}
.pill.common{background:var(--common)}.pill.rarecommon{background:var(--rarecommon)}.pill.trophy{background:var(--trophy)}.pill.raretrophy{background:var(--raretrophy)}.pill.blue{background:var(--blue)}.pill.rareblue{background:var(--rareblue)}
.bar{display:inline-block;width:56px;height:6px;background:#e5e7eb;border-radius:3px;vertical-align:middle;overflow:hidden}
.bar i{display:block;height:100%;background:var(--rareblue)}
.wf{display:inline-block;width:40px;height:6px;background:#e5e7eb;border-radius:3px;vertical-align:middle;overflow:hidden}
.wf i{display:block;height:100%;background:var(--trophy)}
.grade{color:var(--muted)}
.controls{display:flex;gap:10px;flex-wrap:wrap;margin-bottom:18px}
.controls input,.controls select{padding:8px 12px;border:1px solid var(--line);border-radius:8px;font-size:13px;background:#fff}
.controls input{flex:1;min-width:220px}
.summary{display:flex;gap:16px;flex-wrap:wrap;margin-bottom:18px}
.summary div{background:#fff;border:1px solid var(--line);border-radius:10px;padding:10px 16px;min-width:130px}
.summary b{font-size:20px;display:block}.summary small{color:var(--muted)}
@media print{body{background:#fff}.controls{display:none}.water{break-inside:avoid}}
</style></head>
<body>
<header><h1>咖啡厅任务悬赏 · 候选池快照</h1>
<p>生成时间：{{.Generated}} ｜ 共 {{.WaterCount}} 个咖啡厅 ｜ 数值来自预生成候选池，与服务端订单一致</p>
<p>鱼种取自可钓图 · 重量要求=档位线 · 六档金币区间 · 巨物补贴封顶 100 金 ｜ 1 金币 = 100 分</p></header>
<div class="wrap">
<div class="rules">
<b>候选池规则：</b>鱼种来自 <code>loc_fish</code>（可钓图），中文名来自 <code>item_catalog</code>，
重量要求直接取该鱼种档位线：<code>普通/达标→达标线</code>、<code>奖杯→奖杯线</code>、<code>长期位→蓝冠线</code>。
每图 <b>10 槽位</b>，默认 2 奖杯位 + 1 长期位 + 7 普通/达标位（三类占比可在后台调整，总和固定 10），每槽多条候选按现实周期轮换。<br>
<b>交单数量</b>（普通/达标位）：普通鱼 5 条 · 达标鱼 3 条 · 稀有普通鱼 3 条 · 稀有达标鱼 2 条；奖杯/长期位恒 1 条。
<b>轮换时间</b>：长期位 24h · 奖杯位 12h · 普通/达标位逐槽 1~4h。均可在后台修改。<br>
<b>六档金币区间</b>：普通/达标 常见 0.5~2.5 · 稀有 2.5~5 金；奖杯 常见 8~18 · 稀有 18~28 金；蓝冠/长期 常见 18~28 · 稀有 28~48 金。<br>
<b>长期位</b>优先放稀有鱼白名单（七鳃鳗/真柱白鲑/鮟鱇等）与巨物白名单（姥鲨/剑鱼/鲟鳇/六须鲇等），白名单为随程序打包的静态配置；
巨物按蓝冠线分重量级叠加补贴，<b>封顶 100 金</b>。无白名单的水域，长期位回退为本图蓝冠线最高的普通鱼（本图巨物）。
<b>黑名单</b>按水域排除鱼种，命中的鱼不会出现在该水域候选池。
</div>
<div class="legend">
<span><i class="dot" style="background:var(--common)"></i>普通/达标·常见 0.5~2.5 金</span>
<span><i class="dot" style="background:var(--rarecommon)"></i>普通/达标·稀有 2.5~5 金</span>
<span><i class="dot" style="background:var(--trophy)"></i>奖杯·常见 8~18 金</span>
<span><i class="dot" style="background:var(--blue)"></i>蓝冠·常见 18~28 金</span>
<span><i class="dot" style="background:var(--raretrophy)"></i>奖杯·稀有 18~28 金</span>
<span><i class="dot" style="background:var(--rareblue)"></i>长期位·稀有 28~48 金 + 巨物补贴</span>
</div>
<div class="controls">
<input id="q" placeholder="搜索鱼种 / 地图（中英文均可）">
<select id="tier"><option value="">全部梯度</option><option value="0">普通鱼</option><option value="1">稀有普通鱼</option><option value="2">奖杯鱼</option><option value="3">蓝冠鱼</option><option value="4">稀有奖杯鱼</option><option value="5">稀有蓝冠鱼</option></select>
</div>
<div class="summary" id="summary"></div>
<div id="waters"></div>
</div>
<script>
const groups={{.JSON}};
const tierName=['普通鱼','稀有普通鱼','奖杯鱼','蓝冠鱼','稀有奖杯鱼','稀有蓝冠鱼'];
const tierCls=['common','rarecommon','trophy','blue','raretrophy','rareblue'];
const gold=g=>(g/100).toFixed(2);
function render(){
  const q=document.getElementById('q').value.trim().toLowerCase();
  const tf=document.getElementById('tier').value;
  const ws=document.getElementById('waters');
  const cnt=[0,0,0,0,0,0];let slots=0,min=1e9,max=0,sum=0;
  let html='';
  for(const g of groups){
    const rows=g.Slots.filter(s=>{
      const tt=tf===''||String(s.Tier)===tf;
      const match=!q||g.Loc.toLowerCase().includes(q)||g.Level.toLowerCase().includes(q)||s.Species.toLowerCase().includes(q)||s.SpeciesZH.toLowerCase().includes(q);
      return tt&&match;
    });
    if(!rows.length) continue;
    let body='';
    for(const s of rows){
      cnt[s.Tier]++;slots++;min=Math.min(min,s.Gold);max=Math.max(max,s.Gold);sum+=s.Gold;
      const tc=tierCls[s.Tier];
      body+='<tr class="tier-'+tc+' t-'+tc+'">'+
        '<td>'+(s.Ordinal+1)+'</td>'+
        '<td>'+s.SpeciesZH+(s.GiantFish?' <b style="color:#ef4444">巨物</b>':(s.RareFish?' <b style="color:#7c3aed">稀有</b>':''))+'<div class="grade">'+s.Species+'</div></td>'+
        '<td class="grade">'+s.GradeZH+' ('+s.Grade+')</td>'+
        '<td>'+(s.MinWeightG>=1000?(s.MinWeightG/1000).toFixed(2)+' kg':s.MinWeightG+' g')+'</td>'+
        '<td><span class="wf"><i style="width:'+(s.WeightFac*100).toFixed(0)+'%"></i></span> '+s.WeightFac.toFixed(2)+'</td>'+
        '<td>'+s.Quantity+'</td>'+
        '<td>'+s.CatchWt.toFixed(0)+'</td>'+
        '<td><span class="bar"><i style="width:'+(s.Rarity*100).toFixed(0)+'%"></i></span> '+s.Rarity.toFixed(2)+'</td>'+
        '<td><span class="pill '+tc+'">'+tierName[s.Tier]+'</span></td>'+
        '<td class="gold">'+gold(s.Gold)+(s.SubsidyCents>0?'<div class="grade">含补贴 '+gold(s.SubsidyCents)+'</div>':'')+'</td>'+
      '</tr>';
    }
    html+='<div class="water"><h2><span>'+g.Loc+' <small>'+g.Level+'</small></span><small>'+rows.length+' 个任务槽位</small></h2>'+
      '<table><thead><tr><th>#</th><th>鱼种</th><th>最低档位</th><th>要求重量</th><th>重量比因子</th><th>数量</th><th>捕获权重</th><th>稀有度</th><th>梯度</th><th>悬赏(金)</th></tr></thead>'+
      '<tbody>'+body+'</tbody></table></div>';
  }
  ws.innerHTML=html||'<div class="rules">没有匹配的咖啡厅任务。</div>';
  document.getElementById('summary').innerHTML=
    '<div><b>'+slots+'</b><small>任务槽位</small></div>'+
    '<div><b class="t-common">'+cnt[0]+'</b><small>普通鱼</small></div>'+
    '<div><b class="t-rarecommon">'+cnt[1]+'</b><small>稀有普通鱼</small></div>'+
    '<div><b class="t-trophy">'+cnt[2]+'</b><small>奖杯鱼</small></div>'+
    '<div><b class="t-blue">'+cnt[3]+'</b><small>蓝冠鱼</small></div>'+
    '<div><b class="t-raretrophy">'+cnt[4]+'</b><small>稀有奖杯鱼</small></div>'+
    '<div><b class="t-rareblue">'+cnt[5]+'</b><small>稀有蓝冠鱼</small></div>'+
    '<div><b>'+(slots?gold(min):'—')+'~'+(slots?gold(max):'—')+'</b><small>悬赏区间(金)</small></div>'+
    '<div><b>'+(slots?gold(sum/slots):'—')+'</b><small>平均悬赏(金)</small></div>';
}
document.getElementById('q').addEventListener('input',render);
document.getElementById('tier').addEventListener('change',render);
render();
</script></body></html>`
