// Source: synthetic, modelled on real React 18 function-component data-flow
// patterns (destructured props, useState/useReducer state, data fetching,
// conditional rendering) | License: MIT
//
// Used by issue #2855 real-data verification (Data Flow group): proves the
// React extractor emits component_prop entities (generic props, not just nav
// props) and state_setter operations on real-shaped source.

import React, { useState, useReducer, useEffect } from 'react';

interface UserCardProps {
  userId: string;
  title?: string;
  onSelect: (id: string) => void;
}

function reducer(state: number, action: { type: string }) {
  return action.type === 'inc' ? state + 1 : state;
}

export function UserCard({ userId, title = 'User', onSelect }: UserCardProps) {
  const [name, setName] = useState('');
  const [open, setOpen] = useState(false);
  const [count, dispatch] = useReducer(reducer, 0);

  useEffect(() => {
    fetch(`/api/users/${userId}`)
      .then((r) => r.json())
      .then((u) => setName(u.name));
  }, [userId]);

  return (
    <div onClick={() => onSelect(userId)}>
      {open ? <h1>{title}: {name}</h1> : null}
      <button onClick={() => dispatch({ type: 'inc' })}>{count}</button>
      <button onClick={() => setOpen(!open)}>toggle</button>
    </div>
  );
}

export const Avatar = ({ src, alt }: { src: string; alt: string }) => (
  <img src={src} alt={alt} />
);
