# Representative Ruby source-of-truth maps for #4427 in-pipeline validation.
# A frozen constant hash (the cross-graph parity oracle), a constant array,
# a module-level constant group, and a Rails ActiveRecord enum — all of which
# must surface as name-searchable SCOPE.Enum value-sets carrying structured
# {key,value,line} members.

PERMISSION_PAGES = {
  core_admin:         'core-admin',
  contract_proposals: 'contract-proposal',
  users:              'users',
  sync:               'sync',
}.freeze

STATUSES = %w[active inactive archived].freeze

module Roles
  ADMIN   = 'admin'
  MANAGER = 'manager'
  MEMBER  = 'member'
end

class Order < ApplicationRecord
  enum status: { active: 0, archived: 1 }

  PRIORITY_LABELS = { low: 'Low', high: 'High' }.freeze
end
