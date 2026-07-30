const $=s=>document.querySelector(s), esc=v=>String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const unit=(n,u=1)=>{n=Number(n||0)/u;const a=["B","KB","MB","GB","TB"];let i=0;while(n>=1024&&i<4){n/=1024;i++}return `${n.toFixed(n<10&&i?1:0)} ${a[i]}`};
const rate=n=>unit(n)+"/s", pct=n=>`${Number(n||0).toFixed(1)}%`, temp=n=>n?`${Number(n).toFixed(0)}°C`:"—";
const duration=s=>{s=Number(s||0);const d=Math.floor(s/86400),h=Math.floor(s%86400/3600);return `${d} 天 ${h} 小时`};
const bar=n=>`<div class="bar"><i style="width:${Math.min(100,Math.max(0,n||0))}%"></i></div>`;
const empty=t=>`<div class="empty">${t}</div>`;
let snapshot, historyPoints=[];

function metric(label,value,sub,progress){return `<div class="metric"><label>${label}</label><b>${value}</b><small>${sub||""}</small>${progress==null?"":bar(progress)}</div>`}
function table(headers,rows){return `<table><thead><tr>${headers.map(x=>`<th>${x}</th>`).join("")}</tr></thead><tbody>${rows.join("")}</tbody></table>`}
function render(s){
  snapshot=s; const x=s.system||{};
  $("#hostname").textContent=x.hostname||"QNAP NAS";
  $("#identity").textContent=[x.model,x.platform,x.filesystem].filter(Boolean).join(" · ")||"威联通设备";
  const bad=Object.values(s.collectors||{}).filter(v=>!v.ok);
  $("#health").className="health"+(bad.length?" bad":"");
  $("#health").textContent=bad.length?`${bad.length} 项采集异常`:"运行正常";
  $("#updated").textContent=`更新于 ${new Date(s.timestamp).toLocaleTimeString()}`;
  const totalRx=(s.networks||[]).reduce((a,n)=>a+n.rxPerSec,0),totalTx=(s.networks||[]).reduce((a,n)=>a+n.txPerSec,0);
  $("#summary").innerHTML=[
    metric("CPU",pct(x.cpuPercent),`温度 ${temp(x.cpuTemperature)}`,x.cpuPercent),
    metric("内存",pct(x.memoryPercent),`${unit(x.memoryUsed)} / ${unit(x.memoryTotal)}`,x.memoryPercent),
    metric("系统温度",temp(x.temperature),`已运行 ${duration(x.uptimeSeconds)}`),
    metric("网络吞吐",`${rate(totalRx)} ↓`,`${rate(totalTx)} ↑`),
    metric("PCIe 设备",String((s.pcie||[]).length),`硬盘 ${(s.disks||[]).length} · 卷 ${(s.volumes||[]).length}`)
  ].join("");
  $("#fans").innerHTML=(s.fans||[]).length?(s.fans||[]).map(f=>`<div class="mini"><span>${esc(f.name||"风扇 "+f.id)}</span><b>${Number(f.speed||0).toFixed(0)}</b><small>RPM</small></div>`).join(""):empty("SNMP 暂未返回风扇数据");
  $("#disks").innerHTML=(s.disks||[]).length?table(["盘位","型号","状态","SMART","温度","容量"],s.disks.map(d=>`<tr><td>${esc(d.bay||d.id)}</td><td>${esc(d.model)}</td><td>${esc(d.status||"—")}</td><td><span class="pill">${esc(d.smart||"—")}</span></td><td>${temp(d.temperature)}</td><td>${unit(d.capacity)}</td></tr>`)):empty("SNMP 暂未返回硬盘数据");
  $("#volumes").innerHTML=(s.volumes||[]).length?table(["名称","文件系统","状态","已用","总容量","占用"],s.volumes.map(v=>`<tr><td>${esc(v.name)}</td><td>${esc(v.filesystem)}</td><td><span class="pill">${esc(v.status||"Ready")}</span></td><td>${unit(v.used)}</td><td>${unit(v.total)}</td><td>${pct(v.percent)}${bar(v.percent)}</td></tr>`)):empty("暂未发现存储卷");
  $("#networks").innerHTML=(s.networks||[]).length?s.networks.map(n=>`<div class="stack-item"><strong>${esc(n.name)}</strong><span class="right ${n.linkUp?"":"danger"}">${n.linkUp?"已连接":"断开"}</span><small>${n.speedMbps>0?n.speedMbps+" Mbps":"速率未知"} ${esc(n.pcieBdf||"")}</small><small class="right">${rate(n.rxPerSec)} ↓ · ${rate(n.txPerSec)} ↑</small></div>`).join(""):empty("暂未发现主机网卡");
  $("#pcie").innerHTML=(s.pcie||[]).length?s.pcie.map(d=>`<div class="stack-item"><strong>${esc(d.model)}</strong><span class="pill">${esc(d.category)}</span><small>${esc(d.bdf)} · ${esc(d.driver)} · ${esc(d.link)}</small><small class="right">${d.gpuPercent?pct(d.gpuPercent)+" · ":""}${temp(d.temperature)}</small></div>`).join(""):empty("未发现扩展 PCIe 设备；板载设备默认不显示");
  $("#processes").innerHTML=(s.processes||[]).length?table(["PID","用户","进程","CPU","内存","RSS","命令"],s.processes.map(p=>`<tr><td>${p.pid}</td><td>${esc(p.user||p.pid)}</td><td>${esc(p.name)}</td><td>${pct(p.cpuPercent)}</td><td>${pct(p.memoryPercent)}</td><td>${unit(p.rssBytes)}</td><td class="command" title="${esc(p.command)}">${esc(p.command)}</td></tr>`)):empty("首次采样后将显示进程增量");
  $("#shares").innerHTML=(s.shares||[]).length?table(["名称","路径","目录容量"],s.shares.map(v=>`<tr><td>${esc(v.name)}</td><td>${esc(v.path)}</td><td>${v.scanned?unit(v.size):"未启用扫描"}</td></tr>`)):empty("未从 smb.conf 发现共享目录");
}
async function loadStatus(){try{const r=await fetch("/api/status",{cache:"no-store"});if(!r.ok)throw Error(r.status);render(await r.json())}catch(e){$("#health").className="health bad";$("#health").textContent="连接失败"}}
async function loadHistory(){try{const r=await fetch(`/api/history?hours=${$("#historyRange").value}`,{cache:"no-store"});historyPoints=await r.json();draw()}catch(e){}}
function draw(){
  const canvas=$("#trend"),dpr=window.devicePixelRatio||1,rect=canvas.getBoundingClientRect(),w=Math.max(300,rect.width),h=240;
  canvas.width=w*dpr;canvas.height=h*dpr;const c=canvas.getContext("2d");c.scale(dpr,dpr);c.clearRect(0,0,w,h);
  c.strokeStyle="#1e3441";c.lineWidth=1;for(let y=20;y<=220;y+=50){c.beginPath();c.moveTo(0,y+.5);c.lineTo(w,y+.5);c.stroke()}
  if(historyPoints.length<2){c.fillStyle="#8aa0ad";c.fillText("积累两次采样后显示趋势",12,30);return}
  const series=[["cpu","#38d7c4"],["memory","#4ba4ff"],["volumePercent","#f2b84b"]];
  for(const [key,color] of series){c.beginPath();c.strokeStyle=color;c.lineWidth=2;historyPoints.forEach((p,i)=>{const x=i*w/(historyPoints.length-1),y=220-Math.min(100,Math.max(0,p[key]||0))*2;i?c.lineTo(x,y):c.moveTo(x,y)});c.stroke()}
}
$("#historyRange").addEventListener("change",loadHistory);$("#kiosk").addEventListener("click",()=>document.fullscreenElement?document.exitFullscreen():document.documentElement.requestFullscreen());
window.addEventListener("resize",draw);loadStatus();loadHistory();setInterval(loadStatus,5000);setInterval(loadHistory,60000);
