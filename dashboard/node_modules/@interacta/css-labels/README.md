<p align="center" style="color: #444">
  <h1 align="center">CSS Labels</h1>
</p>

Renders HTML labels with built-in intersection logic.

### Quick Start

Install the package:

```bash
npm install @interacta/css-labels
```

Create HTML div element and render labels:

```javascript
import { LabelRenderer } from '@interacta/css-labels'

const div = document.querySelector('#labels')  
const renderer = new LabelRenderer(div)

renderer.setLabels([
  { id: 'monster', text: '👾', x: 100, y: 50, opacity: 1 },
  { id: 'alien', text: '👽', x: 50, y: 150, opacity: 1 },
  { id: 'ufo', text: '🛸', x: 150, y: 150, opacity: 1 },
])
renderer.draw()
```

Or create single Css label:

```javascript
import { CssLabel } from '@interacta/css-labels'
const div = document.querySelector('#labels')

const label = new CssLabel(div, '🐣')
label.setPosition(100, 110)
label.setVisibility(true)
label.setOpacity(1)
label.draw()
```