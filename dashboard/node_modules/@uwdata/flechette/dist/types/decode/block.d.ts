/**
 * Decode a block that points to messages within an Arrow 'file' format.
 * @param {Uint8Array} buf A byte buffer of binary Arrow IPC data
 * @param {number} index The starting index in the byte buffer
 * @returns The file block.
 */
export function decodeBlock(buf: Uint8Array, index: number): {
    offset: number;
    metadataLength: number;
    bodyLength: number;
};
/**
 * Decode a vector of blocks.
 * @param {Uint8Array} buf
 * @param {number} index
 * @returns An array of file blocks.
 */
export function decodeBlocks(buf: Uint8Array, index: number): {
    offset: number;
    metadataLength: number;
    bodyLength: number;
}[];
