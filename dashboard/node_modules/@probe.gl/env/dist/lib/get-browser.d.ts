declare global {
    var chrome: boolean;
    var safari: boolean;
    var mozInnerScreenX: number;
}
export declare function isMobile(): boolean;
export declare function getBrowser(mockUserAgent?: string): 'Node' | 'Electron' | 'Chrome' | 'Firefox' | 'Safari' | 'Edge' | 'Unknown';
//# sourceMappingURL=get-browser.d.ts.map