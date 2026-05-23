import { NumericArray } from '@math.gl/types';
export declare function validateVector(v: NumericArray, length: number): boolean;
export declare function checkNumber(value: unknown): number;
export declare function checkVector<T extends NumericArray>(v: T, length: number, callerName?: string): T;
export declare function deprecated(method: string, version: string): void;
//# sourceMappingURL=validators.d.ts.map