import assert from 'node:assert/strict';
import fs from 'node:fs';

const html = fs.readFileSync(new URL('../internal/webcontrol/web/index.html', import.meta.url), 'utf8');
const css = fs.readFileSync(new URL('../internal/webcontrol/web/style.css', import.meta.url), 'utf8');

assert.match(html, /id="control-surface"[\s\S]*id="transport-direct"[\s\S]*id="special-keys-toggle"[\s\S]*id="pointer-mode"[\s\S]*id="viewport"/, '全屏控制面必须包含传输、特殊按键、鼠标和画面，且特殊按键按钮位于鼠标按钮左侧');
assert.match(css, /body\s*\{[^}]*height:\s*100dvh[^}]*overflow:\s*hidden/s, '窗口模式必须固定为可视高度并禁止页面滚动');
assert.match(css, /main\s*\{[^}]*flex:\s*1[^}]*min-height:\s*0/s, '主画面必须占用剩余可视高度且允许缩小');
assert.match(css, /\.control-surface:fullscreen\s*\{[^}]*width:\s*100vw/s, '全屏模式必须保留完整控制面');
assert.match(css, /\.special-keys\s*\{[^}]*position:\s*absolute/s, '特殊按键必须由按钮呼出为浮动工具栏');

console.log('web layout: viewport fit and fullscreen special-key toolbox verified');
