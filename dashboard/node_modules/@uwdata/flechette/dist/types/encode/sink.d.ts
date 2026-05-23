export class Sink {
    /**
     * Write bytes to this sink.
     * @param {Uint8Array} bytes The byte buffer to write.
     */
    write(bytes: Uint8Array): void;
    /**
     * Write padding bytes (zeroes) to this sink.
     * @param {number} byteCount The number of padding bytes.
     */
    pad(byteCount: number): void;
    /**
     * @returns {Uint8Array | null}
     */
    finish(): Uint8Array | null;
}
export class MemorySink extends Sink {
    buffers: any[];
}
