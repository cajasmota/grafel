/** Magic bytes 'ARROW1' indicating the Arrow 'file' format. */
export const MAGIC: Uint8Array<ArrayBuffer>;
/** Bytes for an 'end of stream' message. */
export const EOS: Uint8Array<ArrayBuffer>;
export namespace Version {
    let V1: 0;
    let V2: 1;
    let V3: 2;
    let V4: 3;
    let V5: 4;
}
export namespace Endianness {
    let Little: 0;
    let Big: 1;
}
export namespace MessageHeader {
    let NONE: 0;
    let Schema: 1;
    let DictionaryBatch: 2;
    let RecordBatch: 3;
    let Tensor: 4;
    let SparseTensor: 5;
}
export namespace Type {
    export let Dictionary: -1;
    let NONE_1: 0;
    export { NONE_1 as NONE };
    export let Null: 1;
    export let Int: 2;
    export let Float: 3;
    export let Binary: 4;
    export let Utf8: 5;
    export let Bool: 6;
    export let Decimal: 7;
    export let Date: 8;
    export let Time: 9;
    export let Timestamp: 10;
    export let Interval: 11;
    export let List: 12;
    export let Struct: 13;
    export let Union: 14;
    export let FixedSizeBinary: 15;
    export let FixedSizeList: 16;
    export let Map: 17;
    export let Duration: 18;
    export let LargeBinary: 19;
    export let LargeUtf8: 20;
    export let LargeList: 21;
    export let RunEndEncoded: 22;
    export let BinaryView: 23;
    export let Utf8View: 24;
    export let ListView: 25;
    export let LargeListView: 26;
}
export namespace Precision {
    let HALF: 0;
    let SINGLE: 1;
    let DOUBLE: 2;
}
export namespace DateUnit {
    let DAY: 0;
    let MILLISECOND: 1;
}
export namespace TimeUnit {
    export let SECOND: 0;
    let MILLISECOND_1: 1;
    export { MILLISECOND_1 as MILLISECOND };
    export let MICROSECOND: 2;
    export let NANOSECOND: 3;
}
export namespace IntervalUnit {
    let YEAR_MONTH: 0;
    let DAY_TIME: 1;
    let MONTH_DAY_NANO: 2;
}
export namespace UnionMode {
    let Sparse: 0;
    let Dense: 1;
}
export namespace CompressionType {
    let LZ4_FRAME: 0;
    let ZSTD: 1;
}
export namespace BodyCompressionMethod {
    let BUFFER: 0;
}
