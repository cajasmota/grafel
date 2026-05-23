/**
 * Create a new table from a collection of columns. Columns are assumed
 * to have the same record batch sizes.
 * @param {[string, Column][] | Record<string, Column>} data The columns,
 *  as an object with name keys, or an array of [name, column] pairs.
 * @param {boolean} [useProxy] Flag indicating if row proxy
 *  objects should be used to represent table rows (default `false`).
 * @returns {Table} The new table.
 */
export function tableFromColumns(data: [string, Column<any>][] | Record<string, Column<any>>, useProxy?: boolean): Table;
import type { Column } from '../column.js';
import { Table } from '../table.js';
