-- Synthetic Neovim plugin fixture based on real nvim-lua patterns.
-- Exercises: vim.api.*, table.*, string.*, math.*, coroutine.*, pcall/xpcall,
-- tostring/tonumber/type, pairs/ipairs, setmetatable/getmetatable, rawget/rawset.

local M = {}

local config = {
    width = 80,
    height = 24,
    timeout = 5000,
    max_items = 100,
}

-- Utility: deep-merge two tables
local function merge_tables(base, override)
    local result = {}
    for k, v in pairs(base) do
        result[k] = v
    end
    for k, v in pairs(override) do
        if type(v) == "table" and type(result[k]) == "table" then
            result[k] = merge_tables(result[k], v)
        else
            result[k] = v
        end
    end
    return result
end

-- Utility: map a function over a list
local function map(tbl, fn)
    local out = {}
    for i, v in ipairs(tbl) do
        out[i] = fn(v)
    end
    return out
end

-- Utility: filter a list
local function filter(tbl, pred)
    local out = {}
    for _, v in ipairs(tbl) do
        if pred(v) then
            table.insert(out, v)
        end
    end
    return out
end

-- Utility: reduce a list
local function reduce(tbl, fn, init)
    local acc = init
    for _, v in ipairs(tbl) do
        acc = fn(acc, v)
    end
    return acc
end

-- Class-like pattern via metatables
local Buffer = {}
Buffer.__index = Buffer

function Buffer.new(bufnr)
    local self = setmetatable({}, Buffer)
    self.bufnr = bufnr or vim.api.nvim_get_current_buf()
    self.lines = {}
    self.ns_id = vim.api.nvim_create_namespace("my_plugin")
    return self
end

function Buffer:load()
    self.lines = vim.api.nvim_buf_get_lines(self.bufnr, 0, -1, false)
    return #self.lines
end

function Buffer:set_lines(start_line, end_line, new_lines)
    vim.api.nvim_buf_set_lines(self.bufnr, start_line, end_line, false, new_lines)
end

function Buffer:add_highlight(line, col_start, col_end, hl_group)
    vim.api.nvim_buf_add_highlight(self.bufnr, self.ns_id, hl_group, line, col_start, col_end)
end

function Buffer:clear_highlights()
    vim.api.nvim_buf_clear_namespace(self.bufnr, self.ns_id, 0, -1)
end

-- Window management
local Window = {}
Window.__index = Window

function Window.new(opts)
    local win_opts = merge_tables({
        relative = "editor",
        row = math.floor((vim.o.lines - config.height) / 2),
        col = math.floor((vim.o.columns - config.width) / 2),
        width = config.width,
        height = config.height,
        style = "minimal",
        border = "rounded",
    }, opts or {})

    local buf = vim.api.nvim_create_buf(false, true)
    local win = vim.api.nvim_open_win(buf, true, win_opts)

    local self = setmetatable({}, Window)
    self.buf = buf
    self.win = win
    return self
end

function Window:close()
    if vim.api.nvim_win_is_valid(self.win) then
        vim.api.nvim_win_close(self.win, true)
    end
    if vim.api.nvim_buf_is_valid(self.buf) then
        vim.api.nvim_buf_delete(self.buf, { force = true })
    end
end

function Window:set_option(name, value)
    vim.api.nvim_win_set_option(self.win, name, value)
end

-- Fuzzy finder core using coroutines
local function make_scorer()
    return coroutine.wrap(function()
        while true do
            local query, candidates = coroutine.yield()
            if query == nil then break end

            local scores = map(candidates, function(c)
                local score = 0
                local qi = 1
                for ci = 1, #c do
                    if string.sub(c, ci, ci) == string.sub(query, qi, qi) then
                        score = score + 1
                        qi = qi + 1
                        if qi > #query then break end
                    end
                end
                return { item = c, score = score }
            end)

            table.sort(scores, function(a, b) return a.score > b.score end)
            local top = filter(scores, function(s) return s.score > 0 end)
            coroutine.yield(top)
        end
    end)
end

-- LSP integration
function M.get_diagnostics(bufnr)
    bufnr = bufnr or vim.api.nvim_get_current_buf()
    local diags = vim.diagnostic.get(bufnr)
    return map(diags, function(d)
        return {
            line = d.lnum + 1,
            col = d.col,
            message = d.message,
            severity = vim.diagnostic.severity[d.severity],
        }
    end)
end

function M.format_diagnostic(diag)
    local parts = {
        string.format("Line %d:%d", diag.line, diag.col),
        string.format("[%s]", diag.severity or "UNKNOWN"),
        diag.message,
    }
    return table.concat(parts, " ")
end

-- String utilities
local function trim(s)
    return string.match(s, "^%s*(.-)%s*$")
end

local function split(s, sep)
    local result = {}
    local pattern = string.format("([^%s]+)", sep)
    for part in string.gmatch(s, pattern) do
        table.insert(result, part)
    end
    return result
end

local function join(tbl, sep)
    return table.concat(tbl, sep or "")
end

-- Config management with validation
function M.setup(opts)
    opts = opts or {}

    -- Validate numeric fields
    for _, field in ipairs({"width", "height", "timeout", "max_items"}) do
        if opts[field] ~= nil then
            local n = tonumber(opts[field])
            if n == nil or n <= 0 then
                error(string.format("invalid config.%s: %s", field, tostring(opts[field])))
            end
            opts[field] = math.floor(n)
        end
    end

    config = merge_tables(config, opts)
    return M
end

-- Async operation via coroutine scheduler
local pending_jobs = {}
local job_id_counter = 0

local function next_job_id()
    job_id_counter = job_id_counter + 1
    return string.format("job-%06d", job_id_counter)
end

function M.schedule_job(fn, on_done)
    local id = next_job_id()
    local co = coroutine.create(fn)
    pending_jobs[id] = {
        co = co,
        on_done = on_done,
        started_at = os.time(),
    }

    local ok, result = coroutine.resume(co)
    if not ok then
        vim.notify(string.format("job %s failed: %s", id, tostring(result)), vim.log.levels.ERROR)
        pending_jobs[id] = nil
        return nil
    end

    if coroutine.status(co) == "dead" then
        if on_done then on_done(result) end
        pending_jobs[id] = nil
    end

    return id
end

function M.cancel_job(id)
    if not pending_jobs[id] then
        return false
    end
    pending_jobs[id] = nil
    return true
end

function M.tick_jobs()
    local now = os.time()
    local expired = filter(vim.tbl_keys(pending_jobs), function(id)
        local job = pending_jobs[id]
        return (now - job.started_at) > math.floor(config.timeout / 1000)
    end)

    for _, id in ipairs(expired) do
        vim.notify("job " .. id .. " timed out", vim.log.levels.WARN)
        pending_jobs[id] = nil
    end

    for id, job in pairs(pending_jobs) do
        if coroutine.status(job.co) ~= "dead" then
            local ok, result = coroutine.resume(job.co)
            if not ok then
                vim.notify(string.format("job %s error: %s", id, tostring(result)), vim.log.levels.ERROR)
                pending_jobs[id] = nil
            elseif coroutine.status(job.co) == "dead" then
                if job.on_done then job.on_done(result) end
                pending_jobs[id] = nil
            end
        end
    end
end

-- Protected call wrapper with logging
function M.safe_call(fn, ...)
    local ok, result = pcall(fn, ...)
    if not ok then
        vim.notify("error: " .. tostring(result), vim.log.levels.ERROR)
        return nil, result
    end
    return result, nil
end

-- Registry with weak values for buffer lifecycle
local registry = setmetatable({}, { __mode = "v" })

function M.register(key, obj)
    rawset(registry, key, obj)
end

function M.lookup(key)
    return rawget(registry, key)
end

-- Math utilities
function M.clamp(val, lo, hi)
    return math.max(lo, math.min(hi, val))
end

function M.lerp(a, b, t)
    return a + (b - a) * t
end

function M.round(n, decimals)
    local mult = math.pow and math.pow(10, decimals or 0) or (10 ^ (decimals or 0))
    return math.floor(n * mult + 0.5) / mult
end

-- Table utilities
function M.keys(tbl)
    local ks = {}
    for k in pairs(tbl) do
        table.insert(ks, k)
    end
    table.sort(ks, function(a, b) return tostring(a) < tostring(b) end)
    return ks
end

function M.values(tbl)
    local vs = {}
    for _, v in pairs(tbl) do
        table.insert(vs, v)
    end
    return vs
end

function M.contains(tbl, val)
    for _, v in ipairs(tbl) do
        if v == val then return true end
    end
    return false
end

return M
