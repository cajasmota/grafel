// React Native fixture (#2859) — FLAGSHIP. Proves React Native's full
// Data Flow/state_management surface (was partial, citing only the detection
// yaml). The state-management extraction lives in the framework-agnostic
// extractor (#513 state setters) + zustand_store.go (#2590 store actions), and
// applies to React Native verbatim. Proves:
//   - state_management   → useState [value, setter], useReducer dispatch, zustand store actions
//   - state_setter_emission → setter / dispatch subtype="state_setter"
//   - context_extraction → createContext()
//   - hoc_wrapper_recognition → memo()
//   - branch_conditions  → discriminator comparisons
import React, { createContext, useState, useReducer, memo } from 'react';
import { View, Text, Pressable } from 'react-native';
import { create } from 'zustand';

// context_extraction
export const CartContext = createContext(null);

// zustand store — actions inside the closure are CALLS targets (#2590)
export const useCartStore = create((set, get) => ({
  items: [],
  addItem: (item) => set((s) => ({ items: [...s.items, item] })),
  clear: () => set({ items: [] }),
}));

function cartReducer(state, action) {
  return state;
}

function CartScreen() {
  // state_management + state_setter_emission
  const [quantity, setQuantity] = useState(1);
  const [coupon, setCoupon] = useState('');
  const [state, dispatch] = useReducer(cartReducer, { items: [] });

  // zustand selector usage
  const addItem = useCartStore((s) => s.addItem);

  // branch_conditions — discriminator comparisons
  function checkout() {
    if (coupon === 'FREESHIP') {
      setQuantity(quantity);
    }
    if (quantity === 0) {
      dispatch({ type: 'reset' });
    }
    addItem({ id: 1 });
  }

  return (
    <View>
      <Text>{quantity}</Text>
      <Pressable onPress={checkout} />
    </View>
  );
}

export default memo(CartScreen);
