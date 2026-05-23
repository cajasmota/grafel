/**
 * Write a file footer.
 * @param {Builder} builder The binary builder.
 * @param {Schema} schema The table schema.
 * @param {Block[]} dictBlocks Dictionary batch file blocks.
 * @param {Block[]} recordBlocks Record batch file blocks.
 * @param {Map<string,string> | null} metadata File-level metadata.
 */
export function writeFooter(builder: Builder, schema: Schema, dictBlocks: Block[], recordBlocks: Block[], metadata: Map<string, string> | null): void;
import type { Builder } from './builder.js';
import type { Schema } from '../types.js';
import type { Block } from '../types.js';
