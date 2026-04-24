import * as vscode from 'vscode';

let costStatusBar: vscode.StatusBarItem;
let agentStatusBar: vscode.StatusBarItem;

export function activate(context: vscode.ExtensionContext) {
    // Register commands.
    const askCmd = vscode.commands.registerCommand('aibutler.ask', async () => {
        const input = await vscode.window.showInputBox({
            prompt: 'Ask AI Butler',
            placeHolder: 'What would you like help with?',
        });
        if (input) {
            await sendToButler('ask', { message: input });
        }
    });

    const explainCmd = vscode.commands.registerCommand('aibutler.explain', async () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor) {
            vscode.window.showWarningMessage('No active editor');
            return;
        }
        const selection = editor.document.getText(editor.selection);
        if (!selection) {
            vscode.window.showWarningMessage('No text selected');
            return;
        }
        await sendToButler('explain', {
            code: selection,
            language: editor.document.languageId,
        });
    });

    const fixCmd = vscode.commands.registerCommand('aibutler.fix', async () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor) {
            vscode.window.showWarningMessage('No active editor');
            return;
        }
        const selection = editor.document.getText(editor.selection);
        const fullText = editor.document.getText();
        await sendToButler('fix', {
            code: selection || fullText,
            language: editor.document.languageId,
            file: editor.document.fileName,
        });
    });

    const testsCmd = vscode.commands.registerCommand('aibutler.tests', async () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor) {
            vscode.window.showWarningMessage('No active editor');
            return;
        }
        const selection = editor.document.getText(editor.selection);
        const fullText = editor.document.getText();
        await sendToButler('tests', {
            code: selection || fullText,
            language: editor.document.languageId,
            file: editor.document.fileName,
        });
    });

    // Status bar items.
    costStatusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
    costStatusBar.text = '$(credit-card) $0.00';
    costStatusBar.tooltip = 'AI Butler cost this session';
    costStatusBar.show();

    agentStatusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 99);
    agentStatusBar.text = '$(hubot) Butler: idle';
    agentStatusBar.tooltip = 'AI Butler agent status';
    agentStatusBar.show();

    context.subscriptions.push(askCmd, explainCmd, fixCmd, testsCmd, costStatusBar, agentStatusBar);
}

export function deactivate() {
    if (costStatusBar) {
        costStatusBar.dispose();
    }
    if (agentStatusBar) {
        agentStatusBar.dispose();
    }
}

async function sendToButler(action: string, payload: Record<string, string>) {
    const config = vscode.workspace.getConfiguration('aibutler');
    const serverUrl = config.get<string>('serverUrl', 'http://localhost:3377');

    agentStatusBar.text = '$(sync~spin) Butler: working...';

    try {
        const response = await fetch(`${serverUrl}/api/vscode/${action}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });

        if (!response.ok) {
            throw new Error(`Server returned ${response.status}`);
        }

        const result = await response.json() as { output?: string };
        if (result.output) {
            const doc = await vscode.workspace.openTextDocument({
                content: result.output,
                language: 'markdown',
            });
            await vscode.window.showTextDocument(doc, { preview: true });
        }
    } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(`AI Butler: ${message}`);
    } finally {
        agentStatusBar.text = '$(hubot) Butler: idle';
    }
}
