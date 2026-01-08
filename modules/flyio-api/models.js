const BaseModel = require('../../src/db/models/BaseModel');

class FlyAccount extends BaseModel {
  static tableName = 'fly_accounts';

  constructor(data) {
    super(FlyAccount.tableName);
    if (data) {
      this.id = data.id;
      this.name = data.name;
      this.api_token = data.api_token;
      this.email = data.email;
      this.organization_id = data.organization_id;
      this.created_at = data.created_at;
      this.updated_at = data.updated_at;
    }
  }
}

module.exports = { FlyAccount };
