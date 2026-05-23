export type ImageData = {
    /** Offset into the data (in addition to any offset built-in to the ArrayBufferView) */
    byteOffset?: number;
    /** The stride, in bytes, between the beginning of each texel block row and the subsequent texel block row. Required if there are multiple texel block rows (i.e. the copy height or depth is more than one block). */
    bytesPerRow?: number;
    /** Number or rows per image (needed if multiple images are being set) */
    rowsPerImage?: number;
    /** Bits per channel */
    bitsPerChannel: [number, number, number, number];
};
export type PixelData = {
    arrayBuffer: ArrayBuffer;
    width: number;
    height: number;
    /** Bytes per pixel */
    bytesPerPixel: number;
    bytesPerRow: number;
    bitsPerChannel: [number, number, number, number];
};
/**
 * Extracts a single RGBA pixel value from PixelData at the given (x, y) coordinate.
 *
 * The pixel's data is assumed to be packed according to pixelData.bitsPerChannel.
 * The pixel data for a given row is padded to pixelData.bytesPerRow.
 *
 * @param pixelData - The metadata and data for the pixel buffer.
 * @param x - The x coordinate (0-based).
 * @param y - The y coordinate (0-based).
 * @returns A tuple [r, g, b, a] where each channel is the extracted numeric value.
 *
* @example

Assume you obtained an ArrayBuffer from copyTextureToBuffer and have the following metadata:

  const pixelData: PixelData = {
    bitsPerChannel: [5, 6, 5, 0], // For example, a 16-bit RGB565 format (no alpha)
    width: 800,
    height: 600,
    bytesPerPixel: 2,           // 16 bits per pixel
    bytesPerRow: 1600,          // Assuming no extra padding
    arrayBuffer: myTextureBuffer, // Obtained from copyTextureToBuffer
  };

You can then extract the pixel at (x, y) like so:

  const rgba = extractPixel(pixelData, x, y);
  console.log("Extracted RGBA:", rgba);

For RGBA formats where all channels are present (e.g. [8, 8, 8, 8]), the function will extract a 4-channel pixel value.
*/
export declare function readPixel(pixelData: PixelData, x: number, y: number, bitsPerChannel: [number, number, number, number]): [number, number, number, number];
/**
 * Encodes an RGBA pixel into a DataView at a given bit offset according to a specified bit layout.
 *
 * The channels are written sequentially in the order R, G, B, A. For each channel, the number
 * of bits is taken from the bitsPerChannel array. Channel values are masked to fit within the specified width.
 *
 * @param dataView - The DataView into which the pixel will be encoded.
 * @param bitOffset - The bit offset in the DataView where the pixel should be written.
 * @param bitsPerChannel - A tuple specifying the number of bits for each channel: [R, G, B, A].
 * @param pixel - A tuple [r, g, b, a] containing the channel values (as numbers).
 *
 * @example

Assume you want to encode a pixel into a packed format where:
  - Red uses 5 bits
  - Green uses 6 bits
  - Blue uses 5 bits
  - Alpha is not used (0 bits)
And the pixel format is packed into 16 bits total.

You might have:
  const bitsPerChannel: [number, number, number, number] = [5, 6, 5, 0];
  const pixel: [number, number, number, number] = [15, 31, 15, 0]; // Example values
  const buffer = new ArrayBuffer(2); // 16 bits = 2 bytes
  const dataView = new DataView(buffer);

Now encode the pixel at bit offset 0:
  encodePixel(dataView, 0, bitsPerChannel, pixel);

The dataView now contains the 16-bit packed pixel value in big-endian order.
*/
export declare function writePixel(dataView: DataView, bitOffset: number, bitsPerChannel: [number, number, number, number], pixel: [number, number, number, number]): void;
/**
 * Reads a specified number of bits from a DataView starting at a given bit offset.
 *
 * For channels with a bit width of 8, 16, or 32 bits and when the bitOffset is byte-aligned,
 * this function uses DataView methods for fast extraction.
 *
 * Bits are assumed to be stored in big-endian order (i.e. the most-significant bit is at position 7 in each byte).
 *
 * @param dataView - The DataView containing the data.
 * @param bitOffset - The offset (in bits) within the data from which to start reading.
 * @param bitCount - The number of bits to read (supported range: 1 to 32).
 * @returns The extracted value as a number.
 */
export declare function readBitsFromDataView(dataView: DataView, bitOffset: number, bitCount: number): number;
/**
 * Writes a specified number of bits from a value into a DataView at a given bit offset.
 *
 * For channels with a bit width of 8, 16, or 32 bits and when the bit offset is byte-aligned,
 * this function uses DataView methods for fast writing.
 *
 * Bits are assumed to be stored in big-endian order (i.e. the most-significant bit is at position 7 in each byte).
 *
 * @param dataView - The DataView to write into.
 * @param bitOffset - The bit offset at which to begin writing.
 * @param bitCount - The number of bits to write (supported range: 1 to 32).
 * @param value - The numeric value whose lower bitCount bits will be written.
 */
export declare function writeBitsToDataView(dataView: DataView, bitOffset: number, bitCount: number, value: number): void;
//# sourceMappingURL=pixel-utils.d.ts.map