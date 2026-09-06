class InvoiceRepository
  def self.all
    []
  end

  def self.create(attrs)
    attrs
  end

  def self.find(id)
    { id: id }
  end

  def self.delete(id)
    id
  end
end
