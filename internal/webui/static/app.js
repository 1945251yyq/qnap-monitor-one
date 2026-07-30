const $=s=>document.querySelector(s), esc=v=>String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const unit=(n,u=1)=>{n=Number(n||0)/u;const a=["B","KB","MB","GB","TB"];let i=0;while(n>=1024&&i<4){n/=1024;i++}return `${n.toFixed(n<10&&i?1:0)} ${a[i]}`};
const rate=n=>unit(n)+"/s", bits=n=>unit(Number(n||0)*8).replace("B","b")+"/s", pct=n=>`${Number(n||0).toFixed(1)}%`, temp=n=>n?`${Number(n).toFixed(0)}°C`:"—";
const duration=s=>{s=Number(s||0);const d=Math.floor(s/86400),h=Math.floor(s%86400/3600),m=Math.floor(s%3600/60);return `${d} 天 ${h} 小时 ${m} 分`};
const bar=n=>`<div class="bar"><i style="width:${Math.min(100,Math.max(0,n||0))}%"></i></div>`;
const empty=t=>`<div class="empty">${t}</div>`;
let historyPoints=[];

function metric(label,value,sub,progress){return `<div class="metric"><label>${label}</label><b>${value}</b><small>${sub||""}</small>${progress==null?"":bar(progress)}</div>`}
function table(headers,rows){return `<table><thead><tr>${headers.map(x=>`<th>${x}</th>`).join("")}</tr></thead><tbody>${rows.join("")}</tbody></table>`}
function maxOf(items,key){return items.reduce((m,item)=>Math.max(m,Number(item[key]||0)),0)}
function sumOf(items,key){return items.reduce((m,item)=>m+Number(item[key]||0),0)}
function render(s){
  const x=s.system||{}, storage=s.storage||{}, disks=s.disks||[], networks=s.networks||[], fans=s.fans||[];
  $("#hostname").textContent=x.hostname||"QNAP NAS";
  $("#identity").textContent=[x.model,x.platform,x.filesystem].filter(Boolean).join(" · ")||"威联通设备";
  const bad=Object.values(s.collectors||{}).filter(v=>!v.ok);
  $("#health").className="health"+(bad.length?" bad":"");
  $("#health").textContent=bad.length?`${bad.length} 项采集异常`:"运行正常";
  $("#updated").textContent=`更新于 ${new Date(s.timestamp).toLocaleTimeString()}`;
  const rxRate=sumOf(networks,"rxPerSec"),txRate=sumOf(networks,"txPerSec"),rxTotal=sumOf(networks,"rxBytes"),txTotal=sumOf(networks,"txBytes");
  const diskMax=maxOf(disks,"temperature"),fanMax=maxOf(fans,"speed");
  $("#summary").innerHTML=[
    metric("采集器 / 运行时间",bad.length?"异常":"正常",duration(x.uptimeSeconds)),
    metric("CPU 使用率",pct(x.cpuPercent),`当前 ${temp(x.cpuTemperature)} · 系统 ${temp(x.temperature)}`,x.cpuPercent),
    metric("内存使用率",pct(x.memoryPercent),`已用 ${unit(x.memoryUsed)} · 可用 ${unit(x.memoryAvailable)}`,x.memoryPercent),
    metric("物理内存",unit(x.memoryTotal),x.zfsArcBytes?`ZFS ARC ${unit(x.zfsArcBytes)} · 可回收 ${unit(x.zfsArcEvictableBytes)}`:""),
    metric("最高硬盘温度",temp(diskMax),`${disks.length} 块硬盘 · 风扇 ${fanMax.toFixed(0)} RPM`),
    metric("共享存储",storage.total?unit(storage.total):"—",`${storage.poolCount||0} 个存储池 · ${storage.shareCount||0} 个共享文件夹`),
    metric("存储占用",storage.total?pct(storage.percent):"—",storage.total?`已用 ${unit(storage.used)} · 可用 ${unit(storage.free)}`:"正在识别",storage.total?storage.percent:null),
    metric("网络实时",`${bits(rxRate)} ↓`,`${bits(txRate)} ↑ · 累计 ${unit(rxTotal)} / ${unit(txTotal)}`)
  ].join("");
  $("#fans").innerHTML=fans.length?fans.map(f=>`<div class="mini"><span>${esc(f.name||"风扇 "+f.id)}</span><b>${Number(f.speed||0).toFixed(0)}</b><small>RPM</small></div>`).join(""):empty("SNMP 暂未返回风扇数据");
  $("#disks").innerHTML=disks.length?table(["盘位","型号","状态","SMART","温度","容量"],disks.map(d=>`<tr><td>${esc(d.bay||d.id)}</td><td>${esc(d.model)}</td><td>${esc(d.status||"—")}</td><td><span class="pill">${esc(d.smart||"—")}</span></td><td>${temp(d.temperature)}</td><td>${unit(d.capacity)}</td></tr>`)):empty("SNMP 暂未返回硬盘数据");
  $("#shares").innerHTML=(s.shares||[]).length?table(["共享文件夹","所在卷","实际路径","占用","占存储"],s.shares.map(v=>`<tr><td><strong>${esc(v.name)}</strong></td><td>${esc(v.volumeName||"—")}</td><td title="${esc(v.realPath||v.path)}">${esc(v.path)}</td><td>${v.scanned?unit(v.size):"正在后台统计"}</td><td>${v.scanned&&storage.total?pct(v.size*100/storage.total):"—"}</td></tr>`)):empty("未从 smb.conf 发现有效共享文件夹");
  $("#volumes").innerHTML=(s.volumes||[]).length?table(["内部数据集","文件系统","状态","已用","总容量","占用"],s.volumes.map(v=>`<tr><td>${esc(v.name)}</td><td>${esc(v.filesystem)}</td><td><span class="pill">${esc(v.status||"Ready")}</span></td><td>${unit(v.used)}</td><td>${unit(v.total)}</td><td>${pct(v.percent)}</td></tr>`)):empty("暂未发现内部数据集");
  $("#networks").innerHTML=networks.length?networks.map(n=>`<div class="stack-item"><strong>${esc(n.name)}</strong><span class="right ${n.linkUp?"":"danger"}">${n.linkUp?"已连接":"断开"}</span><small>${n.speedMbps>0?n.speedMbps+" Mbps":"速率未知"} ${esc(n.pcieBdf||"")}</small><small class="right">${bits(n.rxPerSec)} ↓ · ${bits(n.txPerSec)} ↑<br>累计 ${unit(n.rxBytes)} / ${unit(n.txBytes)}</small></div>`).join(""):empty("暂未发现主机网卡");
  $("#pcie").innerHTML=(s.pcie||[]).length?s.pcie.map(d=>`<div class="stack-item"><strong>${esc(d.model)}</strong><span class="pill">${esc(d.category)}</span><small>${esc(d.bdf)} · ${esc(d.driver)} · ${esc(d.link)} · ${esc(d.capability)}</small><small class="right">${d.gpuPercent?pct(d.gpuPercent)+" GPU · ":""}${d.memoryTotal?pct(d.memoryPercent)+" VRAM · ":""}${temp(d.temperature)}</small></div>`).join(""):empty("未发现扩展 PCIe 设备；板载设备默认不显示");
  $("#processes").innerHTML=(s.processes||[]).length?table(["PID","用户","进程","CPU","内存","RSS","命令"],s.processes.map(p=>`<tr><td>${p.pid}</td><td>${esc(p.user||p.pid)}</td><td>${esc(p.name)}</td><td>${pct(p.cpuPercent)}</td><td>${pct(p.memoryPercent)}</td><td>${unit(p.rssBytes)}</td><td class="command" title="${esc(p.command)}">${esc(p.command)}</td></tr>`)):empty("首次采样后将显示进程增量");
  $("#collectorStatus").innerHTML=Object.entries(s.collectors||{}).map(([name,v])=>`<div class="stack-item"><strong>${esc(name)}</strong><span class="right ${v.ok?"":"danger"}">${v.ok?"正常":"异常"}</span><small>${esc(v.message)}</small><small class="right">${new Date(v.updatedAt).toLocaleTimeString()}</small></div>`).join("");
}
async function loadStatus(){try{const r=await fetch("/api/status",{cache:"no-store"});if(!r.ok)throw Error(r.status);render(await r.json())}catch(e){$("#health").className="health bad";$("#health").textContent="连接失败"}}
async function loadHistory(){try{const r=await fetch(`/api/history?hours=${$("#historyRange").value}`,{cache:"no-store"});historyPoints=await r.json();drawAll()}catch(e){}}
function stats(keys){
  return keys.map(key=>{const values=historyPoints.map(p=>Number(p[key]||0)).filter(Number.isFinite);return {key,last:values.at(-1)||0,max:Math.max(0,...values),mean:values.length?values.reduce((a,b)=>a+b,0)/values.length:0}})}
function drawChart(id,series,formatter,maximum){
  const canvas=$(id),dpr=window.devicePixelRatio||1,rect=canvas.getBoundingClientRect(),w=Math.max(300,rect.width),h=220,pad={l:42,r:8,t:15,b:22};
  canvas.width=w*dpr;canvas.height=h*dpr;const c=canvas.getContext("2d");c.scale(dpr,dpr);c.clearRect(0,0,w,h);c.font="11px system-ui";
  if(historyPoints.length<2){c.fillStyle="#8aa0ad";c.fillText("积累两次采样后显示趋势",12,30);return}
  const values=series.flatMap(s=>historyPoints.map(p=>Number(p[s.key]||0))),max=maximum||Math.max(1,...values)*1.12;
  c.strokeStyle="#1e3441";c.fillStyle="#718793";for(let i=0;i<=4;i++){const y=pad.t+(h-pad.t-pad.b)*i/4;c.beginPath();c.moveTo(pad.l,y+.5);c.lineTo(w-pad.r,y+.5);c.stroke();c.fillText(formatter(max*(4-i)/4),2,y+4)}
  for(const s of series){c.beginPath();c.strokeStyle=s.color;c.lineWidth=2;historyPoints.forEach((p,i)=>{const x=pad.l+i*(w-pad.l-pad.r)/(historyPoints.length-1),y=h-pad.b-Math.max(0,Number(p[s.key]||0))/max*(h-pad.t-pad.b);i?c.lineTo(x,y):c.moveTo(x,y)});c.stroke()}
  const first=new Date(historyPoints[0].time*1000),last=new Date(historyPoints.at(-1).time*1000);c.fillStyle="#718793";c.fillText(first.toLocaleString([], {month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit"}),pad.l,h-3);const end=last.toLocaleTimeString([], {hour:"2-digit",minute:"2-digit"});c.fillText(end,w-pad.r-c.measureText(end).width,h-3)
}
function renderStats(id,series,formatter){
  $(id).innerHTML=stats(series.map(s=>s.key)).map((v,i)=>`<span style="--dot:${series[i].color}">${series[i].label}　当前 ${formatter(v.last)}　最高 ${formatter(v.max)}　平均 ${formatter(v.mean)}</span>`).join("");
}
function drawAll(){
  const percentSeries=[{key:"cpu",label:"CPU",color:"#38d7c4"},{key:"memory",label:"内存",color:"#4ba4ff"}];
  const tempSeries=[{key:"cpuTemp",label:"CPU",color:"#ff6378"},{key:"systemTemp",label:"系统",color:"#f2b84b"},{key:"diskMaxTemp",label:"最高硬盘",color:"#b57cff"}];
  const fanSeries=[{key:"fanRpm",label:"风扇",color:"#38d7c4"}],netSeries=[{key:"networkRx",label:"下载",color:"#4ba4ff"},{key:"networkTx",label:"上传",color:"#f2b84b"}];
  drawChart("#usageTrend",percentSeries,v=>`${v.toFixed(0)}%`,100);renderStats("#usageStats",percentSeries,v=>pct(v));
  drawChart("#temperatureTrend",tempSeries,v=>`${v.toFixed(0)}°`,null);renderStats("#temperatureStats",tempSeries,v=>temp(v));
  drawChart("#fanTrend",fanSeries,v=>`${v.toFixed(0)}`,null);renderStats("#fanStats",fanSeries,v=>`${v.toFixed(0)} RPM`);
  drawChart("#networkTrend",netSeries,v=>unit(v),null);renderStats("#networkStats",netSeries,v=>rate(v));
}
$("#historyRange").addEventListener("change",loadHistory);$("#kiosk").addEventListener("click",()=>document.fullscreenElement?document.exitFullscreen():document.documentElement.requestFullscreen());
window.addEventListener("resize",drawAll);loadStatus();loadHistory();setInterval(loadStatus,5000);setInterval(loadHistory,60000);
