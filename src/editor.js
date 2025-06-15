// main.js - CodeMirror 6 Collaborative Editor Implementation
// import { EditorView, basicSetup } from "codemirror"
import { EditorState, StateEffect, StateField } from "@codemirror/state"
import { Decoration, DecorationSet, WidgetType, EditorView } from "@codemirror/view"
import { lineNumbers } from "@codemirror/view"
import { javascript } from "@codemirror/lang-javascript"
import { collab, sendableUpdates, collapseUpdates, receiveUpdates, getSyncedVersion } from "@codemirror/collab"

// Collaborative Editor Class
class CollaborativeEditor {
    constructor() {
        this.ws = null;
        this.clientId = null;
        this.username = null;
        this.users = new Map();
        this.remoteCursors = new Map();
        this.remoteSelections = new Map();
        this.isConnected = false;
        this.version = 0;
        this.view = null;

        this.initializeCodeMirror();
    }

    initializeCodeMirror() {
        // Remote cursor effect
        const addRemoteCursor = StateEffect.define();
        const removeRemoteCursor = StateEffect.define();
        const updateRemoteCursor = StateEffect.define();

        // Remote selection effect  
        const addRemoteSelection = StateEffect.define();
        const removeRemoteSelection = StateEffect.define();
        const updateRemoteSelection = StateEffect.define();

        // State field for remote cursors
        const remoteCursorField = StateField.define({
            create() { return Decoration.none },
            update(cursors, tr) {
                cursors = cursors.map(tr.changes);

                for (let effect of tr.effects) {
                    if (effect.is(addRemoteCursor)) {
                        const { clientId, position, username } = effect.value;
                        const colorIndex = this.getUserColorIndex(clientId);
                        const widget = new RemoteCursorWidget(username, colorIndex);
                        const deco = Decoration.widget({
                            widget,
                            side: 1
                        });
                        cursors = cursors.update({
                            add: [deco.range(position)]
                        });
                    } else if (effect.is(removeRemoteCursor)) {
                        // Remove cursor for specific client
                        cursors = cursors.update({
                            filter: (from, to, value) => {
                                return !value.spec.widget.clientId === effect.value;
                            }
                        });
                    } else if (effect.is(updateRemoteCursor)) {
                        const { clientId, position, username } = effect.value;
                        // Remove old cursor and add new one
                        cursors = cursors.update({
                            filter: (from, to, value) => {
                                return !value.spec.widget.clientId === clientId;
                            }
                        });
                        const colorIndex = this.getUserColorIndex(clientId);
                        const widget = new RemoteCursorWidget(username, colorIndex, clientId);
                        const deco = Decoration.widget({
                            widget,
                            side: 1
                        });
                        cursors = cursors.update({
                            add: [deco.range(position)]
                        });
                    }
                }

                return cursors;
            },
            provide: f => EditorView.decorations.from(f)
        });

        // State field for remote selections
        const remoteSelectionField = StateField.define({
            create() { return Decoration.none },
            update(selections, tr) {
                selections = selections.map(tr.changes);

                for (let effect of tr.effects) {
                    if (effect.is(addRemoteSelection)) {
                        const { clientId, from, to } = effect.value;
                        if (from !== to) {
                            const colorIndex = this.getUserColorIndex(clientId);
                            const deco = Decoration.mark({
                                class: `cm-remote-selection cm-remote-selection-${colorIndex}`,
                                clientId
                            });
                            selections = selections.update({
                                add: [deco.range(from, to)]
                            });
                        }
                    } else if (effect.is(removeRemoteSelection)) {
                        selections = selections.update({
                            filter: (from, to, value) => {
                                return value.spec.clientId !== effect.value;
                            }
                        });
                    } else if (effect.is(updateRemoteSelection)) {
                        const { clientId, from, to } = effect.value;
                        // Remove old selection
                        selections = selections.update({
                            filter: (from, to, value) => {
                                return value.spec.clientId !== clientId;
                            }
                        });
                        // Add new selection if it's not empty
                        if (from !== to) {
                            const colorIndex = this.getUserColorIndex(clientId);
                            const deco = Decoration.mark({
                                class: `cm-remote-selection cm-remote-selection-${colorIndex}`,
                                clientId
                            });
                            selections = selections.update({
                                add: [deco.range(from, to)]
                            });
                        }
                    }
                }

                return selections;
            },
            provide: f => EditorView.decorations.from(f)
        });

        // Shared effects for collaboration
        const sharedEffects = [
            addRemoteCursor,
            removeRemoteCursor,
            updateRemoteCursor,
            addRemoteSelection,
            removeRemoteSelection,
            updateRemoteSelection
        ];

        // Update listener for collaboration
        const collaborativeExtension = EditorView.updateListener.of((update) => {
            console.log('Editor update:', update);
            if (update.docChanged && this.isConnected) {
                this.sendDocumentChanges(update);
            }
            if (update.selectionSet && this.isConnected) {
                this.sendSelection();
            }
        });

        // Create initial state
        const startState = EditorState.create({
            doc: `// Welcome to the Collaborative CodeMirror Editor!
// Start typing to see real-time collaboration in action.

function hello() {
    console.log('Hello, collaborative world!');
}

hello();

// Features:
// - Real-time collaborative editing with operational transformation
// - Shared cursors with tooltips showing usernames  
// - Shared selections with color coding
// - Line numbers and full CodeMirror 6 functionality
// - WebSocket-based communication with Go backend

class CollaborativeFeatures {
    constructor() {
        this.users = new Map();
        this.cursors = new Map();
        this.selections = new Map();
    }
    
    addUser(clientId, username) {
        this.users.set(clientId, {
            username,
            color: this.getUserColor(clientId)
        });
    }
    
    updateCursor(clientId, position) {
        this.cursors.set(clientId, position);
    }
    
    updateSelection(clientId, from, to) {
        this.selections.set(clientId, { from, to });
    }
    
    getUserColor(clientId) {
        const colors = ['#ef4444', '#10b981', '#f59e0b', '#8b5cf6', '#06b6d4'];
        return colors[Math.abs(clientId.charCodeAt(0)) % colors.length];
    }
}

// Example usage:
const collaborativeEditor = new CollaborativeFeatures();
collaborativeEditor.addUser('user1', 'Alice');
collaborativeEditor.updateCursor('user1', 42);
collaborativeEditor.updateSelection('user1', 10, 20);`,
            extensions: [
                // basicSetup,
                lineNumbers(),
                javascript(),
                collab({ startVersion: this.version }),
                remoteCursorField,
                remoteSelectionField,
                collaborativeExtension,
                EditorView.theme({
                    "&": {
                        height: "600px",
                        fontSize: "14px"
                    },
                    ".cm-scroller": {
                        fontFamily: "Consolas, Monaco, 'Courier New', monospace"
                    },
                    ".cm-content": {
                        padding: "10px"
                    },
                    ".cm-remote-cursor": {
                        borderLeft: "2px solid",
                        height: "1.2em",
                        pointerEvents: "none",
                        position: "relative"
                    },
                    ".cm-remote-cursor-0": { borderColor: "#ef4444" },
                    ".cm-remote-cursor-1": { borderColor: "#10b981" },
                    ".cm-remote-cursor-2": { borderColor: "#f59e0b" },
                    ".cm-remote-cursor-3": { borderColor: "#8b5cf6" },
                    ".cm-remote-cursor-4": { borderColor: "#06b6d4" },
                    ".cm-remote-selection": {
                        backgroundColor: "rgba(59, 130, 246, 0.2)"
                    },
                    ".cm-remote-selection-0": { backgroundColor: "rgba(239, 68, 68, 0.2)" },
                    ".cm-remote-selection-1": { backgroundColor: "rgba(16, 185, 129, 0.2)" },
                    ".cm-remote-selection-2": { backgroundColor: "rgba(245, 158, 11, 0.2)" },
                    ".cm-remote-selection-3": { backgroundColor: "rgba(139, 92, 246, 0.2)" },
                    ".cm-remote-selection-4": { backgroundColor: "rgba(6, 182, 212, 0.2)" }
                })
            ]
        });

        // Create the editor view
        this.view = new EditorView({
            state: startState,
            parent: document.getElementById('editor')
        });

        // Store effects for later use
        this.addRemoteCursor = addRemoteCursor;
        this.removeRemoteCursor = removeRemoteCursor;
        this.updateRemoteCursor = updateRemoteCursor;
        this.addRemoteSelection = addRemoteSelection;
        this.removeRemoteSelection = removeRemoteSelection;
        this.updateRemoteSelection = updateRemoteSelection;

        console.log('CodeMirror 6 collaborative editor initialized');
    }

    connect(username) {
        this.username = username;
        this.ws = new WebSocket('ws://localhost:8080/ws');

        this.ws.onopen = () => {
            console.log('Connected to collaboration server');
            this.isConnected = true;
            this.updateStatus('Connected', true);

            this.send({
                type: 'join',
                username: this.username
            });
        };

        this.ws.onmessage = (event) => {
            console.log('Message from server:', event.data);

            // Handle multiple JSON messages separated by newlines
            const messages = event.data.trim().split('\n');
            for (const messageStr of messages) {
                if (messageStr.trim()) {
                    try {
                        const message = JSON.parse(messageStr);
                        this.handleMessage(message);
                    } catch (error) {
                        console.error('Error parsing message:', messageStr, error);
                    }
                }
            }
        };

        this.ws.onclose = () => {
            console.log('Disconnected from server');
            this.isConnected = false;
            this.updateStatus('Disconnected', false);
            this.clearRemoteElements();
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            this.updateStatus('Connection Error', false);
        };
    }

    send(message) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            const jsonMessage = JSON.stringify(message);
            console.log('Sending to server:', jsonMessage);
            this.ws.send(jsonMessage);
        } else {
            console.warn('WebSocket not connected, cannot send message:', message);
        }
    }

    handleMessage(message) {
        console.log('Handling message:', message);

        switch (message.type) {
            case 'welcome':
                this.clientId = message.clientId;
                this.version = message.version || 0;
                console.log('Received welcome, clientId:', this.clientId, 'version:', this.version);
                if (message.document && message.document !== this.view.state.doc.toString()) {
                    this.applyDocument(message.document);
                }
                break;

            case 'operation':
                console.log('Received operation from:', message.clientId);
                this.applyOperation(message);
                break;

            case 'selection':
                console.log('Received selection from:', message.clientId, 'cursor:', message.cursor);
                this.updateRemoteSelection(message);
                break;

            case 'userJoined':
                console.log('User joined:', message.username, 'clientId:', message.clientId);
                this.users.set(message.clientId, {
                    username: message.username || message.clientId,
                    color: this.getUserColor(message.clientId)
                });
                this.updateUsersList();
                break;

            case 'userLeft':
                console.log('User left:', message.clientId);
                this.users.delete(message.clientId);
                this.removeRemoteUser(message.clientId);
                this.updateUsersList();
                break;

            case 'users':
                console.log('Received users list:', message.users);
                this.users.clear();
                message.users.forEach(user => {
                    if (user.clientId !== this.clientId) {
                        this.users.set(user.clientId, {
                            username: user.username,
                            color: this.getUserColor(user.clientId)
                        });
                    }
                });
                this.updateUsersList();
                break;

            default:
                console.warn('Unknown message type:', message.type);
        }
    }

    sendDocumentChanges(update) {
        // Get sendable updates from CodeMirror collab
        console.log('listener Sending document changes:', update);
        const updates = sendableUpdates(this.view.state);
        if (updates.length > 0) {
            for (let update of updates) {
                const message = {
                    type: 'operation',
                    version: getSyncedVersion(this.view.state),
                    operation: {
                        retain: 0,
                        delete: 0,
                        insert: this.view.state.doc.toString() // Send full document for now
                    }
                };
                console.log('Sending operation:', message);
                this.send(message);
            }
        } else {
            // Fallback: send simple operation with full document
            const message = {
                type: 'operation',
                version: this.version,
                operation: {
                    retain: 0,
                    delete: 0,
                    insert: this.view.state.doc.toString()
                }
            };
            console.log('Sending fallback operation:', message);
            this.send(message);
            this.version++;
        }
    }

    sendSelection() {
        const selection = this.view.state.selection.main;
        const message = {
            type: 'selection',
            from: selection.from,
            to: selection.to,
            cursor: selection.head
        };
        console.log('Sending selection:', message);
        this.send(message);
    }

    applyDocument(document) {
        this.view.dispatch({
            changes: { from: 0, to: this.view.state.doc.length, insert: document }
        });
        // if (document !== this.view.state.doc.toString()) {
        //     const newState = EditorState.create({
        //         doc: document,
        //         extensions: this.view.state.extensions
        //     });
        //     this.view.setState(newState);
        // }
    }

    applyOperation(message) {
        if (message.clientId === this.clientId) return;

        try {
            // Apply updates using CodeMirror collab
            if (message.updates && message.updates.length > 0) {
                const tr = receiveUpdates(this.view.state, message.updates);
                if (tr) {
                    this.view.dispatch(tr);
                }
            }
        } catch (error) {
            console.error('Error applying operation:', error);
        }
    }

    updateRemoteSelection(message) {
        if (message.clientId === this.clientId) return;

        const user = this.users.get(message.clientId);
        if (!user) return;

        // Update cursor
        this.view.dispatch({
            effects: [
                this.updateRemoteCursor.of({
                    clientId: message.clientId,
                    position: message.cursor,
                    username: user.username
                })
            ]
        });

        // Update selection
        this.view.dispatch({
            effects: [
                this.updateRemoteSelection.of({
                    clientId: message.clientId,
                    from: message.from,
                    to: message.to
                })
            ]
        });
    }

    removeRemoteUser(clientId) {
        this.view.dispatch({
            effects: [
                this.removeRemoteCursor.of(clientId),
                this.removeRemoteSelection.of(clientId)
            ]
        });
    }

    clearRemoteElements() {
        this.users.forEach((_, clientId) => {
            this.removeRemoteUser(clientId);
        });
        this.users.clear();
        this.updateUsersList();
    }

    getUserColor(clientId) {
        const colors = ['#ef4444', '#10b981', '#f59e0b', '#8b5cf6', '#06b6d4'];
        return colors[this.getUserColorIndex(clientId)];
    }

    getUserColorIndex(clientId) {
        return Math.abs(clientId.split('').reduce((a, b) => a + b.charCodeAt(0), 0)) % 5;
    }

    updateStatus(text, connected) {
        const statusText = document.getElementById('statusText');
        const statusDot = document.getElementById('statusDot');
        if (statusText) statusText.textContent = text;
        if (statusDot) {
            statusDot.className = `status-dot ${connected ? '' : 'disconnected'}`;
        }
    }

    updateUsersList() {
        const usersList = document.getElementById('usersList');
        if (!usersList) return;

        usersList.innerHTML = '';

        // Add current user
        if (this.username) {
            const userItem = document.createElement('div');
            userItem.className = 'user-item';
            userItem.innerHTML = `
                <div class="user-color" style="background: #2563eb"></div>
                <span>${this.username} (You)</span>
            `;
            usersList.appendChild(userItem);
        }

        // Add remote users
        this.users.forEach((user, clientId) => {
            const userItem = document.createElement('div');
            userItem.className = 'user-item';
            userItem.innerHTML = `
                <div class="user-color" style="background: ${user.color}"></div>
                <span>${user.username}</span>
            `;
            usersList.appendChild(userItem);
        });
    }
}

// Remote cursor widget implementation
class RemoteCursorWidget extends WidgetType {
    constructor(username, colorIndex, clientId = null) {
        super();
        this.username = username;
        this.colorIndex = colorIndex;
        this.clientId = clientId;
    }

    toDOM() {
        const cursor = document.createElement('span');
        cursor.className = `cm-remote-cursor cm-remote-cursor-${this.colorIndex}`;

        const tooltip = document.createElement('div');
        tooltip.className = 'cursor-tooltip';
        tooltip.textContent = this.username;
        tooltip.style.cssText = `
            position: absolute;
            top: -25px;
            left: 0;
            background: rgba(0, 0, 0, 0.8);
            color: white;
            padding: 2px 6px;
            border-radius: 3px;
            font-size: 11px;
            white-space: nowrap;
            pointer-events: none;
            z-index: 1000;
        `;

        cursor.appendChild(tooltip);
        return cursor;
    }

    ignoreEvent() {
        return false;
    }
}

// Initialize editor and global functions
let editor;

document.addEventListener('DOMContentLoaded', () => {
    editor = new CollaborativeEditor();

    // Global function for joining
    window.setUsername = function () {
        const usernameInput = document.getElementById('usernameInput');
        const joinButton = document.querySelector('.user-input button');

        const username = usernameInput.value.trim();
        if (username && editor) {
            editor.connect(username);
            usernameInput.disabled = true;
            joinButton.disabled = true;
            joinButton.textContent = 'Connected';
        }
    };

    // Allow Enter key to join
    document.getElementById('usernameInput').addEventListener('keypress', function (e) {
        if (e.key === 'Enter') {
            window.setUsername();
        }
    });
});

export { CollaborativeEditor };