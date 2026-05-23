/**
 * Write an IPC message to the builder sink.
 * @param {Builder} builder
 * @param {MessageHeader_} headerType
 * @param {number} headerOffset
 * @param {number} bodyLength
 * @param {Block[]} [blocks]
 */
export function writeMessage(builder: Builder, headerType: MessageHeader_, headerOffset: number, bodyLength: number, blocks?: Block[]): void;
import type { Builder } from './builder.js';
import type { MessageHeader_ } from '../types.js';
import type { Block } from '../types.js';
