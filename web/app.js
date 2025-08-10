let apps = [];
let currentlyViewingLogs = null; // Track which app's logs are currently shown

async function loadApps() {
    try {
        const response = await fetch('/api/apps');
        apps = await response.json();
        renderApps();
    } catch (error) {
        console.error('Error loading apps:', error);
        showFlash('error', 'Error loading apps: ' + error.message);
    }
}

function renderApps() {
    const tbody = document.querySelector('#appsTable tbody');
    
    if (apps.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="7">
                    <div class="empty-state">
                        <h3>No apps configured</h3>
                        <p>Add your first app using the form above</p>
                    </div>
                </td>
            </tr>
        `;
        return;
    }
    
    tbody.innerHTML = '';
    
    apps.forEach(app => {
        const row = document.createElement('tr');
        const isRunning = app.pid && app.pid > 0;
        const status = isRunning ? 'running' : 'stopped';
        const statusClass = isRunning ? 'running' : 'stopped';
        const pidDisplay = isRunning ? app.pid : '-';
        
        // Check if this app's logs are currently being viewed
        const isViewingLogs = currentlyViewingLogs === app.name;
        const logsButtonClass = isViewingLogs ? 'btn-primary' : 'btn-secondary';
        const logsButtonText = isViewingLogs ? 'Hide' : 'Logs';
        
        row.innerHTML = `
            <td>${app.name}</td>
            <td title="${app.command}"><code>${truncate(app.command, 50)}</code></td>
            <td><span class="status-badge ${statusClass}">${status}</span></td>
            <td>${app.port}</td>
            <td>${pidDisplay}</td>
            <td>-</td>
            <td>
                <div class="actions-cell">
                    ${isRunning ? 
                        `<a href="http://localhost:${app.port}" target="_blank" class="btn-open">Open</a>` +
                        `<button class="btn-restart" onclick="restartApp('${app.name}')">Restart</button>` +
                        `<button class="btn-stop" onclick="stopApp('${app.name}')">Stop</button>` +
                        (app.log_file ? `<button class="${logsButtonClass}" onclick="viewLogs('${app.name}')">${logsButtonText}</button>` : '<span></span>') +
                        `<button class="btn-relay" onclick="manageRelay('${app.name}')">Relay</button>` +
                        `<button class="btn-danger" onclick="deleteApp('${app.name}')">Delete</button>` : 
                        `<button class="btn-start" onclick="startApp('${app.name}')">Start</button>` +
                        '<span></span><span></span>' +
                        (app.log_file ? `<button class="${logsButtonClass}" onclick="viewLogs('${app.name}')">${logsButtonText}</button>` : '<span></span>') +
                        `<button class="btn-relay" onclick="manageRelay('${app.name}')">Relay</button>` +
                        `<button class="btn-danger" onclick="deleteApp('${app.name}')">Delete</button>`}
                </div>
            </td>
        `;
        tbody.appendChild(row);
    });
}

function truncate(str, maxLen) {
    return str.length > maxLen ? str.substring(0, maxLen) + '...' : str;
}

// Flash message functions
function showFlash(type, message) {
    const flash = document.getElementById('flashMessage');
    const text = document.getElementById('flashText');
    
    // Remove any existing type classes
    flash.classList.remove('success', 'error', 'info', 'show');
    
    // Add the appropriate type class
    flash.classList.add(type);
    text.textContent = message;
    
    // Show the flash message
    flash.classList.add('show');
    
    // Auto-hide after 3 seconds
    setTimeout(() => {
        flash.classList.remove('show');
    }, 3000);
}

async function addApp() {
    const name = document.getElementById('appName').value.trim();
    const port = parseInt(document.getElementById('appPort').value);
    const command = document.getElementById('appCommand').value.trim();
    
    if (!name || !port || !command) {
        showFlash('error', 'Please fill in all fields');
        return;
    }
    
    if (apps.find(app => app.name === name)) {
        showFlash('error', 'App with this name already exists');
        return;
    }
    
    if (apps.find(app => app.port === port)) {
        showFlash('error', 'Port is already in use');
        return;
    }
    
    try {
        const response = await fetch('/api/apps', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, port, command })
        });
        
        if (response.ok) {
            document.getElementById('appName').value = '';
            document.getElementById('appPort').value = '';
            document.getElementById('appCommand').value = '';
            showFlash('success', `App '${name}' added successfully`);
            loadApps();
        } else {
            const error = await response.text();
            showFlash('error', 'Error adding app: ' + error);
        }
    } catch (error) {
        showFlash('error', 'Error adding app: ' + error.message);
    }
}

async function deleteApp(name) {
    if (!confirm(`Are you sure you want to delete '${name}'?`)) return;
    
    try {
        const response = await fetch('/api/apps/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name })
        });
        
        if (response.ok) {
            showFlash('success', `App '${name}' deleted successfully`);
            loadApps();
        } else {
            const error = await response.text();
            showFlash('error', 'Error deleting app: ' + error);
        }
    } catch (error) {
        showFlash('error', 'Error deleting app: ' + error.message);
    }
}

async function startAllApps() {
    try {
        const response = await fetch('/api/apps/start', { method: 'POST' });
        if (response.ok) {
            showFlash('success', 'All apps started successfully');
            setTimeout(loadApps, 1000);
        } else {
            const error = await response.text();
            showFlash('error', 'Error starting apps: ' + error);
        }
    } catch (error) {
        showFlash('error', 'Error starting apps: ' + error.message);
    }
}

async function stopAllApps() {
    try {
        const response = await fetch('/api/apps/stop', { method: 'POST' });
        if (response.ok) {
            showFlash('success', 'All apps stopped successfully');
            setTimeout(loadApps, 1000);
        } else {
            const error = await response.text();
            showFlash('error', 'Error stopping apps: ' + error);
        }
    } catch (error) {
        showFlash('error', 'Error stopping apps: ' + error.message);
    }
}

async function viewLogs(appName) {
    const logsElement = document.getElementById('logs');
    
    // If clicking the same app's logs button and logs are visible, hide them
    if (currentlyViewingLogs === appName && logsElement.style.display === 'block') {
        logsElement.style.display = 'none';
        currentlyViewingLogs = null;
        // Immediately update button states
        loadApps();
        return;
    }
    
    const app = apps.find(a => a.name === appName);
    if (!app || !app.log_file) {
        showFlash('error', 'No log file available for this app');
        return;
    }
    
    // Immediately update state and button appearance
    currentlyViewingLogs = appName;
    logsElement.style.display = 'block';
    
    // Update button states immediately
    loadApps();
    
    // Show loading indicator in logs
    document.getElementById('logsContent').textContent = 'Loading logs...';
    
    try {
        const response = await fetch(`/api/apps/logs?name=${encodeURIComponent(appName)}`);
        const logs = await response.text();
        
        document.getElementById('logsContent').textContent = logs;
        logsElement.scrollTop = logsElement.scrollHeight;
    } catch (error) {
        document.getElementById('logsContent').textContent = 'Error loading logs: ' + error.message;
    }
}

function manageRelay(appName) {
    // Placeholder function for relay management
    showFlash('info', 'Relay management for ' + appName + ' - Coming soon!');
}

async function restartApp(appName) {
    try {
        // First stop the app
        const stopResponse = await fetch('/api/apps/stop', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ name: appName })
        });
        
        const stopData = await stopResponse.json();
        if (stopData.error) {
            showFlash('error', 'Error stopping app: ' + stopData.error);
            return;
        }
        
        // Wait a moment for the process to fully stop
        await new Promise(resolve => setTimeout(resolve, 1000));
        
        // Then start the app
        const startResponse = await fetch('/api/apps/start', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ name: appName })
        });
        
        const startData = await startResponse.json();
        if (startData.error) {
            showFlash('error', 'Error starting app: ' + startData.error);
        } else {
            showFlash('success', `App '${appName}' restarted successfully`);
            loadApps(); // Refresh the table
        }
    } catch (error) {
        showFlash('error', 'Error restarting app: ' + error.message);
    }
}

function startApp(appName) {
    fetch('/api/apps/start', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name: appName })
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            showFlash('error', 'Error starting app: ' + data.error);
        } else {
            showFlash('success', `App '${appName}' started successfully`);
            loadApps(); // Refresh the table
        }
    })
    .catch(error => {
        showFlash('error', 'Error starting app: ' + error.message);
    });
}

function stopApp(appName) {
    fetch('/api/apps/stop', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name: appName })
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            showFlash('error', 'Error stopping app: ' + data.error);
        } else {
            showFlash('success', `App '${appName}' stopped successfully`);
            loadApps(); // Refresh the table
        }
    })
    .catch(error => {
        showFlash('error', 'Error stopping app: ' + error.message);
    });
}

// Load apps on page load
loadApps();

// Auto-refresh every 5 seconds
setInterval(loadApps, 5000);
