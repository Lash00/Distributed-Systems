const API_URL = "http://127.0.0.1:8081";

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
        
        const masterStatusText = document.getElementById('master-status-text');
        const masterText = document.getElementById('master-text');
        const masterPulse = document.getElementById('master-pulse');
        const masterNav = document.getElementById('master-indicator');
        
        if (data.master_down) {
            masterStatusText.classList.add('down');
            masterPulse.classList.add('danger');
            masterNav.classList.add('danger');
            if (data.role === 'master') {
                masterText.innerText = "Offline (Promoted to Master)";
            } else {
                masterText.innerText = "Offline (Master Elected)";
            }
        } else {
            masterStatusText.classList.remove('down');
            masterPulse.classList.remove('danger');
            masterNav.classList.remove('danger');
            masterText.innerText = "Online";
        }

        if (data.role === 'master') {
            if(!window.lastMasterState) addLog("WARNING: Master node is down! Failover active. This node is promoted to MASTER.");
            window.lastMasterState = true;

            // Show Pending Requests Queue Card
            document.getElementById('temp-master-requests-card').style.display = 'block';
            fetchTempMasterRequests();

            // Dynamically update Write Request card
            document.getElementById('form-card-title').innerText = "Execute Database Operations";
            document.getElementById('form-card-desc').innerText = "This node is now promoted to MASTER due to failover. All operations are executed locally and directly!";
            document.getElementById('btn-tab-req-insert').innerText = "Direct Insert";
            document.getElementById('btn-tab-req-update').innerText = "Direct Update";
            document.getElementById('btn-tab-req-delete').innerText = "Direct Delete";
            document.getElementById('btn-tab-req-smart').innerText = "Direct Smart Query";
            document.getElementById('btn-submit-insert').innerText = "Execute Direct Insert";
            document.getElementById('btn-submit-update').innerText = "Execute Direct Update";
            document.getElementById('btn-submit-delete').innerText = "Execute Direct Delete";
            document.getElementById('btn-submit-smart').innerText = "Execute Direct Smart Query";

            // Dynamically transform Replica Info Card into Master View
            const iconEl = document.getElementById('replica-icon');
            iconEl.innerText = "👑";
            iconEl.style.color = "#facc15";
            iconEl.style.filter = "drop-shadow(0 0 12px rgba(250, 204, 21, 0.6))";

            const titleEl = document.getElementById('replica-title');
            titleEl.innerText = "TEMPORARY MASTER ACTIVE";
            titleEl.style.color = "#facc15";

            document.getElementById('replica-desc').innerText = "The original Master is currently offline. This Slave has been promoted to temporary Master, directing operations and replicating downstream to all active replicas.";

            document.getElementById('replica-policies').innerHTML = `
                <strong style="color: #facc15; display: block; margin-bottom: 0.5rem;">Temporary Master Policies:</strong>
                <ul style="margin: 0; padding-left: 1.2rem; list-style-type: square; line-height: 1.6;">
                    <li>Writes are processed locally and committed instantly.</li>
                    <li>Local changes replicate downstream to all active Slaves in real-time.</li>
                    <li>Strict consistency is maintained across the active replica cluster.</li>
                </ul>
            `;
        } else {
            if(window.lastMasterState) addLog("SUCCESS: Master node is back online. Demoting self to SLAVE replica.");
            window.lastMasterState = false;

            // Hide Pending Requests Queue Card
            document.getElementById('temp-master-requests-card').style.display = 'none';

            // Revert Write Request card
            document.getElementById('form-card-title').innerText = "Propose Data Change";
            document.getElementById('form-card-desc').innerText = "Submit a client addition or update proposal to the Master for approval.";
            document.getElementById('btn-tab-req-insert').innerText = "Propose Insert";
            document.getElementById('btn-tab-req-update').innerText = "Propose Update";
            document.getElementById('btn-tab-req-delete').innerText = "Propose Delete";
            document.getElementById('btn-tab-req-smart').innerText = "Propose Smart Query";
            document.getElementById('btn-submit-insert').innerText = "Submit Insert Proposal";
            document.getElementById('btn-submit-update').innerText = "Submit Update Proposal";
            document.getElementById('btn-submit-delete').innerText = "Submit Delete Proposal";
            document.getElementById('btn-submit-smart').innerText = "Submit Smart Query Proposal";

            // Revert Replica Info Card
            const iconEl = document.getElementById('replica-icon');
            iconEl.innerText = "🔒";
            iconEl.style.color = "#ff9800";
            iconEl.style.filter = "drop-shadow(0 0 8px rgba(255, 152, 0, 0.4))";

            const titleEl = document.getElementById('replica-title');
            titleEl.innerText = "READ-ONLY REPLICA";
            titleEl.style.color = "#ff9800";

            document.getElementById('replica-desc').innerText = "This node is a designated database replica. Direct write operations (Insert, Update, Delete) are disabled here to guarantee strict consistency across the cluster.";

            document.getElementById('replica-policies').innerHTML = `
                <strong style="color: #ff9800; display: block; margin-bottom: 0.5rem;">Replication Policies:</strong>
                <ul style="margin: 0; padding-left: 1.2rem; list-style-type: square; line-height: 1.6;">
                    <li>All write transactions must run on the Master Node.</li>
                    <li>Updates replicate downstream to this node in real-time.</li>
                    <li>Supports local high-performance read queries.</li>
                </ul>
            `;
        }
    } catch (e) {
        console.error(e);
    }
}

async function insertClient() {
    const name = document.getElementById('ins-name').value;
    const balance = parseFloat(document.getElementById('ins-balance').value);
    const city = document.getElementById('ins-city').value;
    
    try {
        const res = await fetch(`${API_URL}/insert`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({name, balance, city})
        });
        const data = await res.json();
        addLog(`FORWARD INSERT: ${name} (Res: ${data.message || data.error})`);
        fetchClients();
    } catch(e) { console.error(e); }
}

async function updateClient() {
    const id = parseInt(document.getElementById('upd-id').value);
    const name = document.getElementById('upd-name').value;
    const balance = parseFloat(document.getElementById('upd-balance').value);
    const city = document.getElementById('upd-city').value;
    
    try {
        const res = await fetch(`${API_URL}/update`, {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id, name, balance, city})
        });
        const data = await res.json();
        addLog(`FORWARD UPDATE: Client ${id} (Res: ${data.message || data.error})`);
        fetchClients();
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
        addLog(`FORWARD DELETE: Client ${id} (Res: ${data.message || data.error})`);
        fetchClients();
    } catch(e) { console.error(e); }
}

// Polling for real-time updates
setInterval(fetchClients, 2000);
setInterval(fetchStatus, 2000);
fetchClients();
fetchStatus();
addLog("System initialized. Slave node running.");

function switchRequestTab(tabId) {
    document.getElementById('btn-tab-req-insert').classList.remove('active');
    document.getElementById('btn-tab-req-update').classList.remove('active');
    document.getElementById('btn-tab-req-delete').classList.remove('active');
    document.getElementById('btn-tab-req-smart').classList.remove('active');
    document.getElementById('tab-req-insert').style.display = 'none';
    document.getElementById('tab-req-update').style.display = 'none';
    document.getElementById('tab-req-delete').style.display = 'none';
    document.getElementById('tab-req-smart').style.display = 'none';

    if (tabId === 'insert') {
        document.getElementById('btn-tab-req-insert').classList.add('active');
        document.getElementById('tab-req-insert').style.display = 'block';
    } else if (tabId === 'update') {
        document.getElementById('btn-tab-req-update').classList.add('active');
        document.getElementById('tab-req-update').style.display = 'block';
    } else if (tabId === 'delete') {
        document.getElementById('btn-tab-req-delete').classList.add('active');
        document.getElementById('tab-req-delete').style.display = 'block';
    } else {
        document.getElementById('btn-tab-req-smart').classList.add('active');
        document.getElementById('tab-req-smart').style.display = 'block';
    }
}

async function submitChangeRequest(type) {
    const resultMsg = document.getElementById('req-result-msg');
    resultMsg.innerText = "Submitting proposal...";
    resultMsg.style.color = "#ff9800";

    let payload = { type: type, data: {} };

    if (type === 'insert') {
        const name = document.getElementById('req-ins-name').value;
        let balanceVal = document.getElementById('req-ins-balance').value;
        balanceVal = balanceVal.replace(/[\$,\s]/g, '');
        const balance = parseFloat(balanceVal) || 0;
        const city = document.getElementById('req-ins-city').value;

        payload.data = { name, balance, city };
    } else if (type === 'update') {
        const id = parseInt(document.getElementById('req-upd-id').value);
        const name = document.getElementById('req-upd-name').value;
        let balanceVal = document.getElementById('req-upd-balance').value;
        balanceVal = balanceVal.replace(/[\$,\s]/g, '');
        const balance = parseFloat(balanceVal) || 0;
        const city = document.getElementById('req-upd-city').value;

        payload.data = { id, name, balance, city };
    } else if (type === 'delete') {
        const id = parseInt(document.getElementById('req-del-id').value);
        payload.type = 'smart_query';
        payload.data = { query: `delete client ${id}` };
    } else if (type === 'smart_query') {
        const query = document.getElementById('req-smart-input').value;
        payload.data = { query };
    }

	// Track actual type for console logging
	const originalType = type;

    try {
        const res = await fetch(`${API_URL}/submit-request`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(payload)
        });
        const data = await res.json();
        if (res.status === 200) {
            if (window.lastMasterState) {
                resultMsg.innerText = `✅ Success: ${data.message || "Executed directly!"}`;
                resultMsg.style.color = "#4ade80";
                addLog(`DIRECT WRITE: Executed ${originalType.toUpperCase()} directly on promoted Master DB.`);
            } else {
                resultMsg.innerText = `✅ Success: Proposal ${data.id} submitted!`;
                resultMsg.style.color = "#4ade80";
                addLog(`PROPOSED ${originalType.toUpperCase()}: Submitted proposal ${data.id} to Master.`);
            }
            
            // Empty the inputs
            if (originalType === 'insert') {
                document.getElementById('req-ins-name').value = '';
                document.getElementById('req-ins-balance').value = '';
                document.getElementById('req-ins-city').value = '';
            } else if (originalType === 'update') {
                document.getElementById('req-upd-id').value = '';
                document.getElementById('req-upd-name').value = '';
                document.getElementById('req-upd-balance').value = '';
                document.getElementById('req-upd-city').value = '';
            } else if (originalType === 'delete') {
                document.getElementById('req-del-id').value = '';
            } else if (originalType === 'smart_query') {
                document.getElementById('req-smart-input').value = '';
            }
        } else {
            resultMsg.innerText = `❌ Error: ${data.error || "Failed to submit"}`;
            resultMsg.style.color = "#ef4444";
        }
    } catch(e) {
        console.error(e);
        resultMsg.innerText = "❌ Master connection error.";
        resultMsg.style.color = "#ef4444";
    }
}

async function fetchTempMasterRequests() {
    try {
        const res = await fetch(`${API_URL}/pending-requests`);
        const data = await res.json();
        
        const listDiv = document.getElementById('temp-master-requests-list');
        listDiv.innerHTML = '';
        
        if (data && data.length > 0) {
            data.forEach(req => {
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
                        <div style="background: rgba(255,255,255,0.08); padding: 6px; border-radius: 4px; font-family: monospace; font-size: 0.85rem; border-left: 3px solid #38bdf8; color: #ffd54f; font-weight: bold; margin-top: 5px;">"${req.data.query}"</div>
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
                
                listDiv.innerHTML += `
                    <div style="background: rgba(255,255,255,0.05); border: 1px solid rgba(255,255,255,0.1); border-radius: 10px; padding: 10px; font-size: 0.85rem; display: flex; flex-direction: column; gap: 5px; text-align: left;">
                        <div style="display: flex; justify-content: space-between; border-bottom: 1px dashed rgba(255,255,255,0.1); padding-bottom: 3px; margin-bottom: 3px; font-weight: bold; color: #facc15;">
                            <span>${req.id}</span>
                            <span>From: ${req.origin_ip}</span>
                        </div>
                        ${detailsHtml}
                        <div style="display: flex; gap: 8px; margin-top: 5px;">
                            <button onclick="approveTempRequest('${req.id}')" style="background:#4ade80; color:#111; padding: 5px 8px; font-size: 0.8rem; margin-bottom: 0; width:auto; height:auto; line-height:1; font-weight: bold; border-radius: 4px; border: none; cursor: pointer;">Accept ✅</button>
                            <button onclick="rejectTempRequest('${req.id}')" style="background:#ef4444; color:white; padding: 5px 8px; font-size: 0.8rem; margin-bottom: 0; width:auto; height:auto; line-height:1; font-weight: bold; border-radius: 4px; border: none; cursor: pointer;">Reject ❌</button>
                        </div>
                    </div>
                `;
            });
        } else {
            listDiv.innerHTML = '<p style="font-size: 0.85rem; color: rgba(255, 255, 255, 0.4); text-align: center;">No pending proposals.</p>';
        }
    } catch(e) {
        console.error(e);
    }
}

async function approveTempRequest(id) {
    try {
        const res = await fetch(`${API_URL}/approve-request`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id})
        });
        const data = await res.json();
        addLog(`TEMP MASTER APPROVE: ${id} (Res: ${data.message || data.error})`);
        fetchTempMasterRequests();
        fetchClients();
    } catch(e) { console.error(e); }
}

async function rejectTempRequest(id) {
    try {
        const res = await fetch(`${API_URL}/reject-request`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id})
        });
        const data = await res.json();
        addLog(`TEMP MASTER REJECT: ${id} (Res: ${data.message || data.error})`);
        fetchTempMasterRequests();
    } catch(e) { console.error(e); }
}
