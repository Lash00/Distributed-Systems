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
        fetchClients();
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
        addLog(`DELETE: Client ${id} (Res: ${data.message || data.error})`);
        fetchClients();
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
        fetchClients();
    } catch(e) { console.error(e); }
}

// Polling for real-time updates
setInterval(fetchClients, 2000);
setInterval(fetchStatus, 2000);
fetchClients();
fetchStatus();
addLog("System initialized. GUI connected to Master Node.");
