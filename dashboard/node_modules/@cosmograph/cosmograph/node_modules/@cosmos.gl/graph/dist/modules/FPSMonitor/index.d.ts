export declare class FPSMonitor {
    private bench;
    constructor(canvas: HTMLCanvasElement);
    begin(): void;
    end(now: number): void;
    destroy(): void;
}
