(function(){
  // ============ Elements ============
  const sidebar=document.getElementById("sidebar");
  const sidebarBackdrop=document.getElementById("sidebar-backdrop");
  const sidebarToggle=document.getElementById("sidebar-toggle");
  const navItems=document.querySelectorAll(".nav-item");
  const panels=document.querySelectorAll(".panel");
  const panelTitle=document.getElementById("panel-title");

  const messages=document.getElementById("messages");
  const welcome=document.getElementById("welcome");
  const form=document.getElementById("input-form");
  const input=document.getElementById("input");
  const darkToggle=document.getElementById("dark-toggle");
  const newChatBtn=document.getElementById("new-chat");
  const connPill=document.getElementById("conn-pill");
  const budgetPill=document.getElementById("budget-pill");
  const agentPill=document.getElementById("agent-pill");
  const fileInput=document.getElementById("file-input");
  const fileLabel=document.getElementById("file-label");
  const filePreview=document.getElementById("file-preview");
  const langSelect=document.getElementById("lang-select");
  const welcomeReset=document.getElementById("welcome-reset");

  let ws=null;
  let typingEl=null;
  let pendingFile=null;
  let hasMessages=false;

  // ============ i18n ============
  const i18nStrings={
    en:{title:"AI Butler",heading:"AI Butler",placeholder:"Type a message…",send:"Send"},
    ar:{title:"بتلر الذكي",heading:"بتلر الذكي",placeholder:"اكتب رسالة…",send:"إرسال"}
  };
  function applyI18n(lang){
    const strings=i18nStrings[lang]||i18nStrings.en;
    document.querySelectorAll("[data-i18n]").forEach(el=>{
      const key=el.getAttribute("data-i18n");
      if(strings[key])el.textContent=strings[key];
    });
    document.querySelectorAll("[data-i18n-placeholder]").forEach(el=>{
      const key=el.getAttribute("data-i18n-placeholder");
      if(strings[key])el.placeholder=strings[key];
    });
    document.documentElement.dir=(lang==="ar")?"rtl":"ltr";
    document.documentElement.lang=lang;
    localStorage.setItem("lang",lang);
  }

  // ============ Theme ============
  function applyTheme(theme){
    document.body.classList.remove("dark","light");
    if(theme==="dark"){document.body.classList.add("dark");darkToggle.textContent="\u2600"}
    else if(theme==="light"){document.body.classList.add("light");darkToggle.textContent="\u263D"}
    else{
      const sysDark=window.matchMedia&&window.matchMedia("(prefers-color-scheme: dark)").matches;
      darkToggle.textContent=sysDark?"\u2600":"\u263D";
    }
    localStorage.setItem("theme",theme);
    document.querySelectorAll(".theme-buttons button").forEach(b=>{
      b.classList.toggle("active",b.getAttribute("data-theme")===theme);
    });
  }
  darkToggle.addEventListener("click",()=>{
    const cur=localStorage.getItem("theme")||"auto";
    const next=cur==="dark"?"light":"dark";
    applyTheme(next);
  });
  document.querySelectorAll(".theme-buttons button").forEach(b=>{
    b.addEventListener("click",()=>applyTheme(b.getAttribute("data-theme")));
  });

  // ============ Sidebar + panels ============
  const PANEL_TITLES={chat:"AI Butler",home:"Home",memories:"Memories",apps:"Connected Apps",missions:"Missions",spending:"Spending",settings:"Settings"};
  function switchPanel(name){
    panels.forEach(p=>{
      p.classList.toggle("active",p.getAttribute("data-panel")===name);
    });
    navItems.forEach(n=>{
      n.classList.toggle("active",n.getAttribute("data-panel")===name);
    });
    panelTitle.textContent=PANEL_TITLES[name]||"AI Butler";
    // Close mobile sidebar after selection
    if(window.innerWidth<=900){closeSidebar()}
    // Lazy-load panel data
    if(name==="home")loadHomeData();
    if(name==="memories")loadMemoriesData();
    if(name==="apps")loadAppsData();
    if(name==="missions"){loadMissionsData();startMissionsPolling()}else{stopMissionsPolling()}
    if(name==="spending")loadSpendingData();
    if(name==="settings")loadSettingsData();
  }
  navItems.forEach(item=>{
    item.addEventListener("click",()=>switchPanel(item.getAttribute("data-panel")));
  });

  function openSidebar(){sidebar.classList.add("open");sidebarBackdrop.classList.add("open");sidebarBackdrop.hidden=false}
  function closeSidebar(){sidebar.classList.remove("open");sidebarBackdrop.classList.remove("open");setTimeout(()=>{sidebarBackdrop.hidden=true},200)}
  sidebarToggle.addEventListener("click",()=>{
    if(sidebar.classList.contains("open"))closeSidebar();
    else openSidebar();
  });
  sidebarBackdrop.addEventListener("click",closeSidebar);

  // Tile click → jump to panel
  document.querySelectorAll(".tile[data-target]").forEach(t=>{
    t.addEventListener("click",()=>switchPanel(t.getAttribute("data-target")));
  });

  // ============ Welcome screen + starter prompts ============
  function showWelcome(){
    if(localStorage.getItem("welcomeDismissed")==="true"){
      welcome.classList.add("hidden");
      return;
    }
    if(!hasMessages){welcome.classList.remove("hidden")}
  }
  function hideWelcome(){welcome.classList.add("hidden")}
  // Starter + quickaction + linklike prompt clicks — any element with data-prompt
  function bindPromptClicks(){
    document.querySelectorAll("[data-prompt]").forEach(el=>{
      if(el.__bound)return;
      el.__bound=true;
      el.addEventListener("click",(e)=>{
        e.preventDefault();
        const prompt=el.getAttribute("data-prompt");
        // Switch to chat panel first
        switchPanel("chat");
        input.value=prompt;
        input.focus();
        input.style.height="auto";
        input.style.height=Math.min(input.scrollHeight,120)+"px";
      });
    });
  }

  welcomeReset.addEventListener("click",()=>{
    localStorage.removeItem("welcomeDismissed");
    hasMessages=false;
    messages.innerHTML="";
    showWelcome();
    switchPanel("chat");
  });

  // ============ New chat ============
  newChatBtn.addEventListener("click",()=>{
    if(messages.children.length>0&&!confirm("Start a new chat? The current conversation will be cleared from view (your memories are preserved)."))return;
    messages.innerHTML="";
    hasMessages=false;
    removeTyping();
    showWelcome();
    switchPanel("chat");
    input.value="";
    input.focus();
  });

  // ============ Connection pill ============
  function setConn(state){
    connPill.classList.remove("conn-connected","conn-connecting","conn-disconnected");
    connPill.classList.add("conn-"+state);
    const label=connPill.querySelector(".conn-label");
    const map={connected:"connected",connecting:"connecting",disconnected:"offline"};
    if(label)label.textContent=map[state]||state;
    connPill.title={connected:"Connected",connecting:"Connecting…",disconnected:"Offline — retrying…"}[state]||state;
  }
  // Budget pill click → jump to spending panel
  budgetPill.addEventListener("click",()=>switchPanel("spending"));

  // ============ File upload ============
  fileInput.addEventListener("change",()=>{
    if(fileInput.files.length>0){
      pendingFile=fileInput.files[0];
      filePreview.textContent="Attached: "+pendingFile.name+" ("+Math.round(pendingFile.size/1024)+" KB)";
      filePreview.hidden=false;
    }
  });
  function uploadFile(file){
    const fd=new FormData();
    fd.append("file",file);
    return fetch("/upload",{method:"POST",body:fd}).then(r=>r.json());
  }

  // ============ WebSocket ============
  function connect(){
    setConn("connecting");
    const proto=location.protocol==="https:"?"wss:":"ws:";
    ws=new WebSocket(proto+"//"+location.host+"/ws");
    ws.onopen=()=>{setConn("connected");removeTyping()};
    ws.onmessage=(e)=>{
      const data=JSON.parse(e.data);
      if(data.type==="typing"){showTyping();return}
      if(data.type==="message"){
        removeTyping();
        addMessage("assistant",data.text);
        refreshBudget();
        // If the user has the Memories/Home panel open, refresh it
        const activePanel=document.querySelector(".panel.active");
        if(activePanel){
          const name=activePanel.getAttribute("data-panel");
          if(name==="home")loadHomeData();
          if(name==="memories")loadMemoriesData();
        }
      }
    };
    ws.onclose=()=>{setConn("disconnected");setTimeout(connect,2000)};
    ws.onerror=()=>{ws.close()};
  }

  function addMessage(role,text){
    if(!hasMessages){hasMessages=true;hideWelcome()}
    const div=document.createElement("div");
    div.className="msg "+role;
    div.textContent=text;
    messages.appendChild(div);
    messages.scrollTop=messages.scrollHeight;
  }
  function showTyping(){
    if(typingEl)return;
    if(!hasMessages){hasMessages=true;hideWelcome()}
    typingEl=document.createElement("div");
    typingEl.className="msg typing";
    typingEl.textContent="Thinking…";
    messages.appendChild(typingEl);
    messages.scrollTop=messages.scrollHeight;
  }
  function removeTyping(){
    if(typingEl){typingEl.remove();typingEl=null}
  }

  // ============ Form submit ============
  form.addEventListener("submit",(e)=>{
    e.preventDefault();
    const text=input.value.trim();
    if(!ws||ws.readyState!==1){addMessage("assistant","(not connected — retrying…)");return}
    if(pendingFile){
      const file=pendingFile;
      pendingFile=null;
      filePreview.hidden=true;
      fileInput.value="";
      addMessage("user",(text||"")+" [file: "+file.name+"]");
      uploadFile(file).then(result=>{
        const msg=text?text+" [attached: "+file.name+"]":"[file uploaded: "+file.name+"]";
        ws.send(JSON.stringify({type:"message",text:msg,file_id:result.file_id}));
      }).catch(()=>{addMessage("assistant","File upload failed.")});
      input.value="";
      input.style.height="auto";
      return;
    }
    if(!text)return;
    addMessage("user",text);
    ws.send(JSON.stringify({type:"message",text:text}));
    input.value="";
    input.style.height="auto";
  });
  input.addEventListener("keydown",(e)=>{
    if(e.key==="Enter"&&!e.shiftKey){e.preventDefault();form.dispatchEvent(new Event("submit"))}
  });
  input.addEventListener("input",()=>{
    input.style.height="auto";
    input.style.height=Math.min(input.scrollHeight,120)+"px";
  });

  // ============ Data fetchers ============
  function fmtUSD(n){
    if(n===0)return "$0.00";
    if(n<10)return "$"+n.toFixed(2);
    if(n<100)return "$"+n.toFixed(1);
    return "$"+Math.round(n);
  }
  function fmtRelative(iso){
    if(!iso)return "";
    const d=new Date(iso);
    if(isNaN(d))return "";
    const s=Math.round((Date.now()-d.getTime())/1000);
    if(s<60)return "just now";
    if(s<3600)return Math.floor(s/60)+"m ago";
    if(s<86400)return Math.floor(s/3600)+"h ago";
    if(s<2592000)return Math.floor(s/86400)+"d ago";
    return d.toLocaleDateString();
  }
  function escapeHtml(s){
    const d=document.createElement("div");
    d.textContent=s;
    return d.innerHTML;
  }

  function refreshBudget(){
    fetch("/api/dashboard/costs").then(r=>r.ok?r.json():null).then(data=>{
      if(!data||!data.costs||data.costs.length===0){budgetPill.hidden=true;return}
      let total=0;
      data.costs.forEach(c=>{total+=(c.cost_usd||0)});
      budgetPill.hidden=false;
      budgetPill.textContent=fmtUSD(total)+" used";
    }).catch(()=>{budgetPill.hidden=true});
  }

  // Home panel — populate all tiles from stats + costs
  function loadHomeData(){
    fetch("/api/dashboard/stats").then(r=>r.ok?r.json():null).then(data=>{
      if(!data)return;
      const tileMem=document.getElementById("tile-memories");
      const tileSess=document.getElementById("tile-sessions");
      if(tileMem)tileMem.textContent=((data.thoughts||0)+(data.entities||0)+(data.key_facts||0)).toString();
      if(tileSess)tileSess.textContent=(data.sessions||0).toString();
    }).catch(()=>{});
    fetch("/api/dashboard/costs").then(r=>r.ok?r.json():null).then(data=>{
      const tileCost=document.getElementById("tile-cost");
      if(!tileCost)return;
      if(!data||!data.costs){tileCost.textContent="$0.00";return}
      let total=0;data.costs.forEach(c=>{total+=(c.cost_usd||0)});
      tileCost.textContent=fmtUSD(total);
    }).catch(()=>{});
    const tileApps=document.getElementById("tile-apps");
    if(tileApps)tileApps.textContent="1"; // webchat is always active — we're in it
  }

  // Memories panel — show mini-stats + notes list
  function loadMemoriesData(){
    fetch("/api/dashboard/stats").then(r=>r.ok?r.json():null).then(data=>{
      if(!data)return;
      const t=document.getElementById("mem-count-thoughts");
      const e=document.getElementById("mem-count-entities");
      const f=document.getElementById("mem-count-facts");
      if(t)t.textContent=(data.thoughts||0).toString();
      if(e)e.textContent=(data.entities||0).toString();
      if(f)f.textContent=(data.key_facts||0).toString();
    }).catch(()=>{});

    fetch("/api/dashboard/memory").then(r=>r.ok?r.json():null).then(data=>{
      const list=document.getElementById("memory-list");
      if(!list)return;
      if(!data||!data.thoughts||data.thoughts.length===0){
        list.innerHTML='<div class="empty-state"><div class="empty-icon">🧠</div><p>No notes saved yet.<br>Tell me anything you want me to remember.</p></div>';
        return;
      }
      list.innerHTML="";
      data.thoughts.forEach(t=>{
        const item=document.createElement("div");
        item.className="memory-item";
        item.innerHTML=
          '<div class="memory-item-header">'+
            '<span class="memory-item-source">'+escapeHtml(t.source||"note")+'</span>'+
            '<span class="memory-item-date">'+escapeHtml(fmtRelative(t.created_at))+'</span>'+
          '</div>'+
          '<div class="memory-item-content">'+escapeHtml(t.content||"")+'</div>';
        list.appendChild(item);
      });
    }).catch(()=>{
      const list=document.getElementById("memory-list");
      if(list)list.innerHTML='<div class="empty-state muted">Couldn\'t load memories.</div>';
    });

    loadFactsData();
    loadConflictsData();
    loadProposalsData();
  }

  // Pending approvals — self-authored skill proposals awaiting a decision.
  function loadProposalsData(){
    const section=document.getElementById("proposals-section");
    const list=document.getElementById("proposals-list");
    if(!section||!list)return;
    fetch("/api/dashboard/proposals").then(r=>r.ok?r.json():null).then(data=>{
      const proposals=(data&&data.proposals)||[];
      if(proposals.length===0){section.classList.add("hidden");return;}
      section.classList.remove("hidden");
      list.innerHTML="";
      proposals.forEach(p=>{
        const item=document.createElement("div");
        item.className="memory-item fact-item";
        item.innerHTML=
          '<div class="memory-item-header">'+
            '<span class="memory-item-source">'+escapeHtml(p.title||("Proposal #"+p.id))+'</span>'+
            '<span class="memory-item-date">'+escapeHtml(fmtRelative(p.created_at))+'</span>'+
          '</div>'+
          '<details class="memory-item-content"><summary>Review the full skill</summary><pre style="white-space:pre-wrap">'+escapeHtml(p.body||"(file unavailable)")+'</pre></details>'+
          '<div class="fact-actions">'+
            '<button type="button" class="linklike" data-act="approve">Approve</button>'+
            '<button type="button" class="linklike danger" data-act="reject">Reject</button>'+
          '</div>';
        item.querySelector('[data-act="approve"]').addEventListener("click",()=>{
          if(confirm("Activate this skill? Review the full content first."))factAction("/api/dashboard/proposals/approve",{id:p.id});
        });
        item.querySelector('[data-act="reject"]').addEventListener("click",()=>{
          factAction("/api/dashboard/proposals/reject",{id:p.id});
        });
        list.appendChild(item);
      });
    }).catch(()=>{section.classList.add("hidden");});
  }

  // Key facts — current beliefs with pin / correct / forget actions.
  function loadFactsData(){
    const list=document.getElementById("facts-list");
    if(!list)return;
    fetch("/api/dashboard/memory/facts").then(r=>r.ok?r.json():null).then(data=>{
      if(!data||!data.facts||data.facts.length===0){
        list.innerHTML='<div class="empty-state muted">No key facts yet. They\'re extracted automatically as we talk.</div>';
        return;
      }
      list.innerHTML="";
      data.facts.forEach(f=>{
        const item=document.createElement("div");
        item.className="memory-item fact-item";
        item.innerHTML=
          '<div class="memory-item-header">'+
            '<span class="memory-item-source">'+escapeHtml(f.category||"fact")+(f.pinned?' 📌':'')+'</span>'+
            '<span class="memory-item-date">'+escapeHtml(fmtRelative(f.extracted_at))+'</span>'+
          '</div>'+
          '<div class="memory-item-content">'+escapeHtml(f.fact||"")+'</div>'+
          '<div class="fact-actions">'+
            '<button type="button" class="linklike" data-act="pin">'+(f.pinned?'Unpin':'Pin')+'</button>'+
            '<button type="button" class="linklike" data-act="correct">Fix</button>'+
            '<button type="button" class="linklike danger" data-act="forget">Forget</button>'+
          '</div>';
        item.querySelector('[data-act="pin"]').addEventListener("click",()=>{
          factAction("/api/dashboard/memory/facts/pin",{id:f.id,pinned:!f.pinned});
        });
        item.querySelector('[data-act="correct"]').addEventListener("click",()=>{
          const fixed=prompt("Correct this fact:",f.fact);
          if(fixed&&fixed.trim()&&fixed!==f.fact)factAction("/api/dashboard/memory/facts/correct",{id:f.id,fact:fixed.trim()});
        });
        item.querySelector('[data-act="forget"]').addEventListener("click",()=>{
          if(confirm("Permanently delete this fact? This cannot be undone."))factAction("/api/dashboard/memory/facts/forget",{fact_id:f.id});
        });
        list.appendChild(item);
      });
    }).catch(()=>{
      list.innerHTML='<div class="empty-state muted">Couldn\'t load facts.</div>';
    });
  }

  // Conflicts — contradictions where a newer statement replaced a confident older one.
  function loadConflictsData(){
    const section=document.getElementById("conflicts-section");
    const list=document.getElementById("conflicts-list");
    if(!section||!list)return;
    fetch("/api/dashboard/memory/conflicts?unreviewed=1").then(r=>r.ok?r.json():null).then(data=>{
      const conflicts=(data&&data.conflicts)||[];
      const pending=conflicts.filter(c=>c.resolution==="needs_review"&&!c.reviewed);
      if(pending.length===0){section.classList.add("hidden");return;}
      section.classList.remove("hidden");
      list.innerHTML="";
      pending.forEach(c=>{
        const item=document.createElement("div");
        item.className="memory-item fact-item";
        item.innerHTML=
          '<div class="memory-item-content">Now: '+escapeHtml(c.new_fact)+'<br><span class="muted">Was: '+escapeHtml(c.old_fact)+'</span></div>'+
          '<div class="fact-actions">'+
            '<button type="button" class="linklike" data-act="keep">Keep new</button>'+
            '<button type="button" class="linklike" data-act="restore">Restore old</button>'+
          '</div>';
        item.querySelector('[data-act="keep"]').addEventListener("click",()=>{
          factAction("/api/dashboard/memory/conflicts/review",{id:c.id});
        });
        item.querySelector('[data-act="restore"]').addEventListener("click",()=>{
          factAction("/api/dashboard/memory/conflicts/restore",{id:c.id});
        });
        list.appendChild(item);
      });
    }).catch(()=>{section.classList.add("hidden");});
  }

  function factAction(url,body){
    fetch(url,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)})
      .then(r=>r.json().catch(()=>({})).then(data=>{
        if(!r.ok&&data.error)alert(data.error);
        else if(data.note)alert(data.note);
      }))
      .catch(()=>{})
      .finally(()=>loadMemoriesData());
  }

  // Connected apps panel — show webchat as active
  function loadAppsData(){
    const active=document.getElementById("apps-active");
    if(!active)return;
    active.innerHTML=
      '<div class="app-card active">'+
        '<span class="app-icon">🌐</span>'+
        '<span class="app-name">Web Chat</span>'+
        '<span class="app-desc">You\'re using it right now</span>'+
        '<span class="app-badge">Connected</span>'+
      '</div>';
  }

  // Spending panel — breakdown per model
  function loadSpendingData(){
    fetch("/api/dashboard/costs").then(r=>r.ok?r.json():null).then(data=>{
      const total=document.getElementById("spending-total");
      const bd=document.getElementById("spending-breakdown");
      if(!total||!bd)return;
      if(!data||!data.costs||data.costs.length===0){
        total.textContent="$0.00";
        bd.innerHTML='<div class="empty-state muted">No spending to report yet.</div>';
        return;
      }
      let totalUSD=0;
      data.costs.forEach(c=>{totalUSD+=(c.cost_usd||0)});
      total.textContent=fmtUSD(totalUSD);
      bd.innerHTML="";
      data.costs.forEach(c=>{
        const item=document.createElement("div");
        item.className="spending-item";
        const tokens=(c.input_tokens||0)+(c.output_tokens||0);
        const niceTokens=tokens>1000?(tokens/1000).toFixed(1)+"k":tokens.toString();
        item.innerHTML=
          '<div>'+
            '<div class="spending-item-name">'+escapeHtml(c.model||"Unknown model")+'</div>'+
            '<div class="spending-item-detail">'+niceTokens+' tokens</div>'+
          '</div>'+
          '<div class="spending-item-cost">'+fmtUSD(c.cost_usd||0)+'</div>';
        bd.appendChild(item);
      });
    }).catch(()=>{
      const bd=document.getElementById("spending-breakdown");
      if(bd)bd.innerHTML='<div class="empty-state muted">Couldn\'t load spending data.</div>';
    });
  }

  // Missions panel — list, detail subview, and live event tail.
  //
  // Polling strategy: a 2-second interval refreshes whichever view is
  // active (list or detail). The interval starts when the panel opens
  // and stops on every panel switch. No SSE / WebSocket here —
  // mission events are append-only and a 2s polling cadence is plenty
  // for a dashboard view. Cuts complexity vs a long-lived stream
  // (no reconnect handling, no stale-cursor logic).
  let missionsPollInterval=null;
  let missionsActiveDetailID=null;

  function missionStateBadge(state){
    const labels={created:"created",planned:"planned",running:"running",waiting_user:"waiting user",completed:"completed",failed:"failed",cancelled:"cancelled"};
    return '<span class="mission-state mission-state-'+escapeHtml(state||"unknown")+'">'+escapeHtml(labels[state]||state||"unknown")+'</span>';
  }

  function loadMissionsData(){
    if(missionsActiveDetailID){loadMissionDetail(missionsActiveDetailID);return}
    fetch("/api/dashboard/missions/stats").then(r=>r.ok?r.json():null).then(stats=>{
      if(!stats)return;
      const set=(id,val)=>{const e=document.getElementById(id);if(e)e.textContent=val};
      set("missions-stat-active",(stats.active||0).toString());
      set("missions-stat-completed",(stats.completed||0).toString());
      set("missions-stat-failed",(stats.failed||0).toString());
      set("missions-stat-cost",fmtUSD(stats.total_cost_usd||0));
    }).catch(()=>{});

    const includeDone=document.getElementById("missions-include-done");
    const params=new URLSearchParams();
    params.set("limit","30");
    if(includeDone&&includeDone.checked)params.set("include_done","true");
    fetch("/api/dashboard/missions?"+params.toString()).then(r=>r.ok?r.json():null).then(rows=>{
      const list=document.getElementById("missions-list");
      if(!list)return;
      if(!rows||rows.length===0){
        list.innerHTML='<div class="empty-state"><div class="empty-icon">🎯</div><p>No missions yet.<br>Create one via the <code>mission.create</code> tool in chat.</p></div>';
        return;
      }
      list.innerHTML="";
      rows.forEach(m=>{
        const item=document.createElement("button");
        item.type="button";
        item.className="mission-item";
        item.setAttribute("data-mission-id",m.id);
        const cost=fmtUSD(m.cost_so_far_usd||0);
        item.innerHTML=
          '<div class="mission-item-row">'+
            '<span class="mission-item-goal">'+escapeHtml(m.goal||"(no goal)")+'</span>'+
            missionStateBadge(m.state)+
          '</div>'+
          '<div class="mission-item-meta muted">'+
            escapeHtml(m.step_count+" steps")+' · '+
            escapeHtml(cost)+' · '+
            escapeHtml(fmtRelative(m.created_at))+
          '</div>';
        item.addEventListener("click",()=>{
          missionsActiveDetailID=m.id;
          showMissionDetail();
          loadMissionDetail(m.id);
        });
        list.appendChild(item);
      });
    }).catch(()=>{
      const list=document.getElementById("missions-list");
      if(list)list.innerHTML='<div class="empty-state muted">Couldn\'t load missions.</div>';
    });
  }

  function showMissionDetail(){
    const list=document.getElementById("missions-list");
    const detail=document.getElementById("mission-detail");
    if(list)list.hidden=true;
    if(detail)detail.hidden=false;
  }
  function hideMissionDetail(){
    const list=document.getElementById("missions-list");
    const detail=document.getElementById("mission-detail");
    if(list)list.hidden=false;
    if(detail)detail.hidden=true;
    missionsActiveDetailID=null;
  }

  function loadMissionDetail(missionID){
    fetch("/api/dashboard/missions/"+encodeURIComponent(missionID)).then(r=>r.ok?r.json():null).then(data=>{
      if(!data||!data.mission)return;
      const m=data.mission;
      const goal=document.querySelector("#mission-detail .mission-detail-goal");
      const meta=document.querySelector("#mission-detail .mission-detail-meta");
      if(goal)goal.textContent=m.goal||"(no goal)";
      if(meta){
        const cost=fmtUSD(m.cost_so_far_usd||0);
        meta.innerHTML=
          missionStateBadge(m.state)+' · '+
          escapeHtml(m.step_count+" steps")+' · '+
          escapeHtml(m.event_count+" events")+' · '+
          escapeHtml(cost)+' · created '+escapeHtml(fmtRelative(m.created_at));
      }
      const stepsEl=document.getElementById("mission-detail-steps");
      if(stepsEl){
        if(!data.steps||data.steps.length===0){
          stepsEl.innerHTML='<div class="empty-state muted">No steps planned yet.</div>';
        }else{
          stepsEl.innerHTML="";
          data.steps.forEach((s,i)=>{
            const row=document.createElement("div");
            row.className="mission-step mission-step-state-"+escapeHtml(s.state||"unknown");
            const errBlock=s.error?'<div class="mission-step-error">'+escapeHtml(s.error)+'</div>':'';
            const outBlock=s.output?'<div class="mission-step-output muted">'+escapeHtml(s.output)+'</div>':'';
            row.innerHTML=
              '<div class="mission-step-row">'+
                '<span class="mission-step-idx">#'+(i+1)+'</span>'+
                '<span class="mission-step-task">'+escapeHtml(s.task||"")+'</span>'+
                missionStateBadge(s.state)+
              '</div>'+
              errBlock+outBlock;
            stepsEl.appendChild(row);
          });
        }
      }
    }).catch(()=>{});

    fetch("/api/dashboard/missions/"+encodeURIComponent(missionID)+"/events?limit=50").then(r=>r.ok?r.json():null).then(events=>{
      const evEl=document.getElementById("mission-detail-events");
      if(!evEl)return;
      if(!events||events.length===0){
        evEl.innerHTML='<div class="empty-state muted">No events yet.</div>';
        return;
      }
      evEl.innerHTML="";
      // Show newest first.
      events.slice().reverse().forEach(e=>{
        const row=document.createElement("div");
        row.className="mission-event";
        const payload=e.payload_json?escapeHtml(e.payload_json):"";
        row.innerHTML=
          '<span class="mission-event-time muted">'+escapeHtml(fmtRelative(e.timestamp))+'</span>'+
          '<span class="mission-event-type">'+escapeHtml(e.event_type)+'</span>'+
          (payload?'<span class="mission-event-payload muted">'+payload+'</span>':'');
        evEl.appendChild(row);
      });
    }).catch(()=>{});
  }

  function startMissionsPolling(){
    if(missionsPollInterval)return;
    missionsPollInterval=setInterval(loadMissionsData,2000);
  }
  function stopMissionsPolling(){
    if(missionsPollInterval){clearInterval(missionsPollInterval);missionsPollInterval=null}
  }

  // Wire up missions panel controls. Idempotent — safe to call once on
  // page load; the listeners stay attached for the page's lifetime.
  function initMissionsPanel(){
    const back=document.getElementById("mission-detail-back");
    if(back)back.addEventListener("click",()=>{
      hideMissionDetail();
      loadMissionsData();
    });
    const refresh=document.getElementById("missions-refresh");
    if(refresh)refresh.addEventListener("click",loadMissionsData);
    const includeDone=document.getElementById("missions-include-done");
    if(includeDone)includeDone.addEventListener("change",()=>{
      missionsActiveDetailID=null;
      hideMissionDetail();
      loadMissionsData();
    });
  }
  initMissionsPanel();

  // Settings panel — show activity summary
  function loadSettingsData(){
    fetch("/api/dashboard/stats").then(r=>r.ok?r.json():null).then(data=>{
      if(!data)return;
      const a=document.getElementById("settings-activity");
      if(!a)return;
      const parts=[];
      if(data.sessions!==undefined)parts.push(data.sessions+" conversations");
      if(data.entities!==undefined)parts.push(data.entities+" things saved");
      if(data.thoughts!==undefined)parts.push(data.thoughts+" notes");
      a.textContent=parts.length?parts.join(" · "):"nothing yet";
    }).catch(()=>{});
  }

  // Language selector
  if(langSelect){
    langSelect.addEventListener("change",()=>applyI18n(langSelect.value));
  }

  // ============ Init ============
  applyTheme(localStorage.getItem("theme")||"auto");
  const savedLang=localStorage.getItem("lang")||document.documentElement.lang||"en";
  applyI18n(savedLang);
  if(langSelect)langSelect.value=savedLang;
  bindPromptClicks();
  showWelcome();
  connect();
  refreshBudget();
  setInterval(refreshBudget,30000);

  // PWA: register the service worker so users can install Butler on their
  // home screen. Registration is best-effort — if it fails (e.g. served
  // over plain HTTP on a non-localhost host, or in an insecure iframe) the
  // app still works, it just isn't installable.
  if("serviceWorker" in navigator){
    window.addEventListener("load",function(){
      navigator.serviceWorker.register("/sw.js",{scope:"/"}).catch(function(err){
        console.warn("PWA service worker registration failed:",err);
      });
    });
  }
})();
