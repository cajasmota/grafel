-- Sample Lua module — golden fixture source.

local M = {}

local users = {
    {id = 1, name = "Alice", email = "alice@example.com"}
}
local next_id = 2

function M.find_all()
    return users
end

function M.find_by_id(id)
    for _, user in ipairs(users) do
        if user.id == id then
            return user
        end
    end
    return nil
end

function M.create(name, email)
    local user = {id = next_id, name = name, email = email}
    next_id = next_id + 1
    table.insert(users, user)
    return user
end

function M.delete(id)
    for i, user in ipairs(users) do
        if user.id == id then
            table.remove(users, i)
            return true
        end
    end
    return false
end

local function validate_email(email)
    return email:match("^[^@]+@[^@]+%.[^@]+$") ~= nil
end

function M.create_validated(name, email)
    if not validate_email(email) then
        return nil, "invalid email"
    end
    return M.create(name, email), nil
end

return M
