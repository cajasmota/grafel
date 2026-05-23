export declare function withResolvers<T>(): {
    promise: Promise<T>;
    resolve: (t: T) => void;
    reject: (error: Error) => void;
};
//# sourceMappingURL=promise-utils.d.ts.map