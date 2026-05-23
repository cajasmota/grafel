import type { DevtoolsButtonPosition } from '@tanstack/query-devtools';
import type { DevtoolsErrorType } from '@tanstack/query-devtools';
import type { DevtoolsPosition } from '@tanstack/query-devtools';
import type { QueryClient } from '@tanstack/react-query';
import * as React_2 from 'react';
import type { Theme } from '@tanstack/query-devtools';

declare namespace Devtools {
    export {
        ReactQueryDevtools,
        DevtoolsOptions
    }
}

export declare interface DevtoolsOptions {
    /**
     * Set this true if you want the dev tools to default to being open
     */
    initialIsOpen?: boolean;
    /**
     * The position of the TanStack logo to open and close the devtools panel.
     * 'top-left' | 'top-right' | 'bottom-left' | 'bottom-right' | 'relative'
     * Defaults to 'bottom-right'.
     */
    buttonPosition?: DevtoolsButtonPosition;
    /**
     * The position of the React Query devtools panel.
     * 'top' | 'bottom' | 'left' | 'right'
     * Defaults to 'bottom'.
     */
    position?: DevtoolsPosition;
    /**
     * Custom instance of QueryClient
     */
    client?: QueryClient;
    /**
     * Use this so you can define custom errors that can be shown in the devtools.
     */
    errorTypes?: Array<DevtoolsErrorType>;
    /**
     * Use this to pass a nonce to the style tag that is added to the document head. This is useful if you are using a Content Security Policy (CSP) nonce to allow inline styles.
     */
    styleNonce?: string;
    /**
     * Use this so you can attach the devtool's styles to specific element in the DOM.
     */
    shadowDOMTarget?: ShadowRoot;
    /**
     * Set this to true to hide disabled queries from the devtools panel.
     */
    hideDisabledQueries?: boolean;
    /**
     * Set this to 'light', 'dark', or 'system' to change the theme of the devtools panel.
     * Defaults to 'system'.
     */
    theme?: Theme;
}

declare namespace DevtoolsPanel {
    export {
        ReactQueryDevtoolsPanel,
        DevtoolsPanelOptions
    }
}

export declare interface DevtoolsPanelOptions {
    /**
     * Custom instance of QueryClient
     */
    client?: QueryClient;
    /**
     * Use this so you can define custom errors that can be shown in the devtools.
     */
    errorTypes?: Array<DevtoolsErrorType>;
    /**
     * Use this to pass a nonce to the style tag that is added to the document head. This is useful if you are using a Content Security Policy (CSP) nonce to allow inline styles.
     */
    styleNonce?: string;
    /**
     * Use this so you can attach the devtool's styles to specific element in the DOM.
     */
    shadowDOMTarget?: ShadowRoot;
    /**
     * Custom styles for the devtools panel
     * @default { height: '500px' }
     * @example { height: '100%' }
     * @example { height: '100%', width: '100%' }
     */
    style?: React_2.CSSProperties;
    /**
     * Callback function that is called when the devtools panel is closed
     */
    onClose?: () => void;
    /**
     * Set this to true to hide disabled queries from the devtools panel.
     */
    hideDisabledQueries?: boolean;
    /**
     * Set this to 'light', 'dark', or 'system' to change the theme of the devtools panel.
     * Defaults to 'system'.
     */
    theme?: Theme;
}

export declare type DevtoolsPanelOptions_alias_1 = DevtoolsPanel.DevtoolsPanelOptions;

export declare function ReactQueryDevtools(props: DevtoolsOptions): React_2.ReactElement | null;

export declare const ReactQueryDevtools_alias_1: (typeof Devtools)['ReactQueryDevtools'];

export declare const ReactQueryDevtools_alias_2: typeof Devtools.ReactQueryDevtools;

export declare function ReactQueryDevtoolsPanel(props: DevtoolsPanelOptions): React_2.ReactElement | null;

export declare const ReactQueryDevtoolsPanel_alias_1: (typeof DevtoolsPanel)['ReactQueryDevtoolsPanel'];

export declare const ReactQueryDevtoolsPanel_alias_2: typeof DevtoolsPanel.ReactQueryDevtoolsPanel;

export { }
