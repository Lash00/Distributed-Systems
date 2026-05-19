const API_URL = "http://127.0.0.1:8080";

function switchTab(tabId) {
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
    
    event.target.classList.add('active');
    document.getElementById('tab-' + tabId).classList.add('active');
}

function addLog(msg) {
    const logs = document.getElementById('logs');
    const entry = document.createElement('div');
    entry.className = 'log-entry';
    entry.innerText = `[${new Date().toLocaleTimeString()}] ${msg}`;
    logs.prepend(entry);
}

async function fetchClients() {
    try {
        const res = await fetch(`${API_URL}/clients`);
        const data = await res.json();
        
        const tbody = document.querySelector('#clients-table tbody');
        tbody.innerHTML = '';
        
        if (data && data.length > 0) {
            document.getElementById('client-count').innerText = data.length;
            data.forEach(c => {
                tbody.innerHTML += `
                    <tr>
                        <td>${c.id}</td>
                        <td>${c.name}</td>
                        <td>$${c.balance}</td>
                        <td>${c.city}</td>
                    </tr>
                `;
            });
        } else {
            document.getElementById('client-count').innerText = "0";
        }
    } catch (e) {
        console.error(e);
    }
}

async function fetchStatus() {
    try {
        const res = await fetch(`${API_URL}/status`);
        const data = await res.json();
        
        const list = document.getElementById('slave-list');
        list.innerHTML = '';
        
        if (data.active_slaves) {
            document.getElementById('slave-count').innerText = data.active_slaves.length;
            data.active_slaves.forEach(s => {
                list.innerHTML += `<li><span class="indicator" style="display:inline-block; width:8px; height:8px;"></span> ${s}</li>`;
            });
        } else {
            document.getElementById('slave-count').innerText = "0";
        }

        // --- Failback Sync Banner Logic ---
        const banner = document.getElementById('sync-banner');
        const bannerIcon = document.getElementById('sync-banner-icon');
        const bannerText = document.getElementById('sync-banner-text');

        if (data.sync_complete) {
            // Show green success banner
            if (!window._syncBannerShown) {
                banner.style.display = 'flex';
                banner.style.background = 'linear-gradient(135deg, rgba(74, 222, 128, 0.2), rgba(34, 197, 94, 0.1))';
                banner.style.border = '1px solid rgba(74, 222, 128, 0.5)';
                banner.style.color = '#4ade80';
                bannerIcon.innerText = '\u2705';
                bannerText.innerText = 'Failback sync complete! All data written by the Temp Master has been applied. Master is now fully in control.';
                addLog('FAILBACK: Sync from temp-slave complete. Master DB is up-to-date.');
                window._syncBannerShown = true;
                // Auto-hide after 10 seconds
                setTimeout(() => { banner.style.display = 'none'; }, 10000);
            }
        } else if (!window._syncBannerShown && window._masterStarted) {
            // Show yellow waiting banner only if master just came back (flag set below)
            banner.style.display = 'flex';
            banner.style.background = 'linear-gradient(135deg, rgba(250, 204, 21, 0.15), rgba(245, 158, 11, 0.1))';
            banner.style.border = '1px solid rgba(250, 204, 21, 0.4)';
            banner.style.color = '#facc15';
            bannerIcon.innerText = '\u26a0\ufe0f';
            bannerText.innerText = 'Failback mode: Waiting for Temp Slave to push sync data...';
        }
        window._masterStarted = true;
    } catch (e) {
        console.error(e);
    }
}

async function insertClient() {
    const name = document.getElementById('ins-name').value;
    let balanceVal = document.getElementById('ins-balance').value;
    // Clean currency symbols, commas, and whitespace
    balanceVal = balanceVal.replace(/[\$,\s]/g, '');
    const balance = parseFloat(balanceVal) || 0;
    const city = document.getElementById('ins-city').value;
    
    try {
        const res = await fetch(`${API_URL}/insert`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({name, balance, city})
        });
        const data = await res.json();
        addLog(`INSERT: ${name} (Res: ${data.message || data.error})`);
        if (res.status === 200) {
            document.getElementById('ins-name').value = '';
            document.getElementById('ins-balance').value = '';
            document.getElementById('ins-city').value = '';
            fetchClients();
        } else {
            alert(`⚠️ Insertion Error: ${data.error}`);
        }
    } catch(e) { console.error(e); }
}

async function updateClient() {
    const id = parseInt(document.getElementById('upd-id').value);
    const name = document.getElementById('upd-name').value;
    let balanceVal = document.getElementById('upd-balance').value;
    // Clean currency symbols, commas, and whitespace
    balanceVal = balanceVal.replace(/[\$,\s]/g, '');
    const balance = parseFloat(balanceVal) || 0;
    const city = document.getElementById('upd-city').value;
    
    try {
        const res = await fetch(`${API_URL}/update`, {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id, name, balance, city})
        });
        const data = await res.json();
        addLog(`UPDATE: Client ${id} (Res: ${data.message || data.error})`);
        if (res.status === 200) {
            document.getElementById('upd-id').value = '';
            document.getElementById('upd-name').value = '';
            document.getElementById('upd-balance').value = '';
            document.getElementById('upd-city').value = '';
            fetchClients();
        } else {
            alert(`⚠️ Update Error: ${data.error}`);
        }
    } catch(e) { console.error(e); }
}

async function deleteClient() {
    const id = parseInt(document.getElementById('del-id').value);
    
    try {
        const res = await fetch(`${API_URL}/delete`, {
            method: 'DELETE',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id})
        });
        const data = await res.json();
        addLog(`DELETE: Client ${id} (Res: ${data.message || data.error})`);
        if (res.status === 200) {
            document.getElementById('del-id').value = '';
            fetchClients();
        } else {
            alert(`⚠️ Deletion Error: ${data.error}`);
        }
    } catch(e) { console.error(e); }
}

async function executeNLP() {
    const text = document.getElementById('nlp-input').value;
    try {
        const res = await fetch(`${API_URL}/text-to-query`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({text})
        });
        const data = await res.json();
        document.getElementById('nlp-result').innerText = data.message || data.error;
        addLog(`QUERY: "${text}" -> ${data.message || data.error}`);
        if (res.status === 200) {
            document.getElementById('nlp-input').value = '';
            fetchClients();
        } else {
            alert(`⚠️ Query Error: ${data.error}`);
        }
    } catch(e) { console.error(e); }
}

// Polling for real-time updates
setInterval(fetchClients, 2000);
setInterval(fetchStatus, 2000);
setInterval(fetchPendingRequests, 2000);
fetchClients();
fetchStatus();
fetchPendingRequests();
addLog("System initialized. GUI connected to Master Node.");

async function fetchPendingRequests() {
    try {
        const res = await fetch(`${API_URL}/pending-requests`);
        const data = await res.json();
        
        const listDiv = document.getElementById('pending-requests-list');
        listDiv.innerHTML = '';

        const inheritedBadge = document.getElementById('inherited-badge');
        
        if (data && data.length > 0) {
            // Check if any are inherited from the temp master
            const hasInherited = data.some(r => r.id && r.id.startsWith('INHERITED-'));
            if (inheritedBadge) inheritedBadge.style.display = hasInherited ? 'inline-block' : 'none';

            data.forEach(req => {
                const isInherited = req.id && req.id.startsWith('INHERITED-');
                let detailsHtml = '';
                if (req.type === 'insert') {
                    detailsHtml = `
                        <div><strong>Type:</strong> <span style="color:#4ade80; font-weight:bold;">PROPOSE INSERT</span></div>
                        <div><strong>Client:</strong> ${req.data.name}</div>
                        <div><strong>Balance:</strong> $${req.data.balance}</div>
                        <div><strong>City:</strong> ${req.data.city}</div>
                    `;
                } else if (req.type === 'smart_query') {
                    detailsHtml = `
                        <div><strong>Type:</strong> <span style="color:#38bdf8; font-weight:bold;">PROPOSE SMART QUERY</span></div>
                        <div style="background: rgba(255,255,255,0.08); padding: 6px; border-radius: 4px; font-family: monospace; font-size: 0.85rem; border-left: 3px solid #38bdf8; color: #ffd54f; font-weight: bold; margin-top: 5px;">${req.data.query}</div>
                    `;
                } else {
                    detailsHtml = `
                        <div><strong>Type:</strong> <span style="color:#ff9800; font-weight:bold;">PROPOSE UPDATE</span></div>
                        <div><strong>ID:</strong> ${req.data.id}</div>
                        <div><strong>New Name:</strong> ${req.data.name}</div>
                        <div><strong>New Balance:</strong> $${req.data.balance}</div>
                        <div><strong>New City:</strong> ${req.data.city}</div>
                    `;
                }
                
                // Inherited badge for requests that came from the temp master
                const inheritedTag = isInherited
                    ? `<span style="background:#ff9800; color:#111; font-size:0.7rem; font-weight:bold; padding:2px 6px; border-radius:4px; margin-left:6px;">&#9651; INHERITED FROM TEMP MASTER</span>`
                    : '';

                listDiv.innerHTML += `
                    <div style="background: ${isInherited ? 'rgba(255,152,0,0.07)' : 'rgba(255,255,255,0.05)'}; border: 1px solid ${isInherited ? 'rgba(255,152,0,0.4)' : 'rgba(255,255,255,0.1)'}; border-radius: 10px; padding: 10px; font-size: 0.85rem; display: flex; flex-direction: column; gap: 5px;">
                        <div style="display: flex; justify-content: space-between; border-bottom: 1px dashed rgba(255,255,255,0.1); padding-bottom: 3px; margin-bottom: 3px; font-weight: bold; color: var(--primary-acc);">
                            <span>${req.id} ${inheritedTag}</span>
                            <span>From: ${req.origin_ip} &bull; ${req.timestamp}</span>
                        </div>
                        ${detailsHtml}
                        <div style="display: flex; gap: 8px; margin-top: 5px;">
                            <button onclick="approveRequest('${req.id}')" style="background:#4ade80; color:#111; padding: 5px 8px; font-size: 0.8rem; margin-bottom: 0; width:auto; height:auto; line-height:1;">Accept &#10003;</button>
                            <button onclick="rejectRequest('${req.id}')" style="background:#ef4444; color:white; padding: 5px 8px; font-size: 0.8rem; margin-bottom: 0; width:auto; height:auto; line-height:1;">Reject &#10005;</button>
                        </div>
                    </div>
                `;
            });
        } else {
            if (inheritedBadge) inheritedBadge.style.display = 'none';
            listDiv.innerHTML = `
                <div style="color: #aaa; font-style: italic; font-size: 0.9rem; text-align: center; padding: 1rem;">
                    No pending write requests.
                </div>
            `;
        }
    } catch (e) {
        console.error(e);
    }
}

async function approveRequest(id) {
    try {
        const res = await fetch(`${API_URL}/approve-request`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id})
        });
        const data = await res.json();
        addLog(`APPROVE: Request ${id} approved (Res: ${data.message || data.error})`);
        fetchPendingRequests();
        fetchClients();
    } catch(e) { console.error(e); }
}

async function rejectRequest(id) {
    try {
        const res = await fetch(`${API_URL}/reject-request`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id})
        });
        const data = await res.json();
        addLog(`REJECT: Request ${id} rejected (Res: ${data.message || data.error})`);
        fetchPendingRequests();
    } catch(e) { console.error(e); }
}
