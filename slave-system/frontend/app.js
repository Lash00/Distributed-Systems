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
            masterText.innerText = "Offline";
            if(!window.lastMasterState) addLog("WARNING: Master node is down!");
            window.lastMasterState = true;
        } else {
            masterStatusText.classList.remove('down');
            masterPulse.classList.remove('danger');
            masterNav.classList.remove('danger');
            masterText.innerText = "Online";
            if(window.lastMasterState) addLog("SUCCESS: Master node is back online.");
            window.lastMasterState = false;
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
