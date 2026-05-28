// Store.tsx — proving fixture for issue #2894 PR1 React Ecosystem group:
// redux_store_extraction + redux_async_flow. Mixes classic Redux, Redux
// Toolkit (createSlice/configureStore/createAsyncThunk/createEntityAdapter),
// react-redux hooks, and a redux-saga effects file shape.
import { createStore, combineReducers } from 'redux';
import { connect } from 'react-redux';
import {
  configureStore,
  createSlice,
  createAsyncThunk,
  createEntityAdapter,
} from '@reduxjs/toolkit';
import { useSelector, useDispatch, useStore } from 'react-redux';
import { takeEvery, takeLatest, put, call, select } from 'redux-saga/effects';

// --- Redux Toolkit async thunk (redux_async_flow) ---
export const fetchUser = createAsyncThunk('user/fetch', async (id: string) => {
  const res = await fetch(`/api/users/${id}`);
  return res.json();
});

// --- Redux Toolkit entity adapter ---
const usersAdapter = createEntityAdapter();

// --- Redux Toolkit slice (redux_store_extraction) ---
export const userSlice = createSlice({
  name: 'user',
  initialState: usersAdapter.getInitialState({ loading: false }),
  reducers: {
    setName: (state, action) => {
      state.name = action.payload;
    },
    clearUser: (state) => {
      state.name = undefined;
    },
  },
  extraReducers: (builder) => {
    builder.addCase(fetchUser.fulfilled, (state, action) => {
      usersAdapter.setOne(state, action.payload);
    });
  },
});

export const { setName, clearUser } = userSlice.actions;

// --- classic Redux combineReducers + createStore ---
const rootReducer = combineReducers({
  user: userSlice.reducer,
});

export const legacyStore = createStore(rootReducer);

// --- Redux Toolkit configureStore (redux_store_extraction) ---
export const store = configureStore({
  reducer: {
    user: userSlice.reducer,
  },
});

// --- react-redux hooks component ---
export function UserName() {
  const name = useSelector((s: any) => s.user.name);
  const dispatch = useDispatch();
  const reduxStore = useStore();
  return null;
}

// --- classic connect HOC ---
const mapStateToProps = (state: any) => ({ name: state.user.name });
const mapDispatchToProps = (dispatch: any) => ({
  setName: (n: string) => dispatch(setName(n)),
});
export const ConnectedUserName = connect(mapStateToProps, mapDispatchToProps)(UserName);

// --- redux-saga effects (redux_async_flow) ---
function* loadUserSaga(action: any) {
  const id = yield select((s: any) => s.user.id);
  const data = yield call(fetch, `/api/users/${id}`);
  yield put(setName(data.name));
}

export function* rootSaga() {
  yield takeEvery('user/load', loadUserSaga);
  yield takeLatest('user/refresh', loadUserSaga);
}
