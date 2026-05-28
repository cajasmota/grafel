// NativeScript core fixture (#2859) — vanilla NativeScript uses its OWN
// component model: a view-model class extends Observable and mutates state via
// `set` accessors / this.set("prop", v) / notifyPropertyChange, which the
// extractor recognises as state setters (subtype="state_setter"). Proves:
//   - Data Flow/state_management      → Observable setter methods
//   - Lifecycle/state_setter_emission → subtype="state_setter" on those methods
//   - Data Flow/branch_conditions     → discriminator comparisons in a body
import { Observable } from '@nativescript/core';

export class MainViewModel extends Observable {
  private _counter = 0;
  private _status = 'idle';

  // state setter — `set` accessor that notifies observers
  set counter(value: number) {
    this._counter = value;
    this.notifyPropertyChange('counter', value);
  }

  get counter(): number {
    return this._counter;
  }

  // state setter — imperative this.set("prop", v)
  incrementCounter() {
    this.set('counter', this._counter + 1);
  }

  // state setter — direct notifyPropertyChange
  reset() {
    this._counter = 0;
    this.notifyPropertyChange('counter', 0);
  }

  // NOT a state setter — plain method, no observable notification.
  // branch_conditions: discriminator comparisons on bare identifiers.
  classify() {
    const status = this._status;
    if (status === 'idle') {
      return 0;
    }
    if (this._counter === 10) {
      return 2;
    }
    return 1;
  }
}
