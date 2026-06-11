class ContractsController < ApplicationController
  def create_contact
    contract = Contract.find_by(id: params[:id])
    return head :not_found if contract.nil?
    if contract.client.nil?
      render status: 400, json: { error: "contract has no client" }
      return
    end
    existing = Contact.find_by(email: params[:email])
    if existing.present?
      render status: 409, json: { error: "contact already exists" }
      return
    end
    begin
      contact = contract.contacts.create!(contact_params)
      render status: 201, json: contact
    rescue ActiveRecord::RecordInvalid => e
      Rails.logger.error(e)
      render status: 500, json: { error: "failed to create contact" }
    end
  end

  def update_contact
    contact = Contact.find_by(id: params[:contact_id])
    return head :unprocessable_entity if contact.nil?
    conflict = Contact.find_by(email: params[:email])
    if conflict.present? && conflict.id != contact.id
      render status: 422, json: { error: "email conflict" }
      return
    end
    begin
      contact.update!(contact_params)
      render status: 200, json: contact
    rescue ActiveRecord::RecordInvalid => e
      Rails.logger.error(e)
      render status: 503, json: { error: "failed to update contact" }
    end
  end
end
