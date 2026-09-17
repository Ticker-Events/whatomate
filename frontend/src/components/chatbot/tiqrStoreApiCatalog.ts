export type TiqrStoreOperation =
  | 'list_collections'
  | 'search_collections'
  | 'list_products'
  | 'search_products'
  | 'get_product'
  | 'list_product_options'
  | 'get_store'
  | 'get_store_info'
  | 'list_faqs'
  | 'check_delivery'
  | 'create_order'
  | 'get_order'
  | 'lookup_order_status'
  | 'retry_payment'

export type TiqrStoreField = {
  key: string
  label: string
  required?: boolean
  placeholder?: string
  hint?: string
  multiline?: boolean
}

export type TiqrStoreOperationDef = {
  value: TiqrStoreOperation
  label: string
  fields: TiqrStoreField[]
}

export const TIQR_STORE_OPERATIONS: TiqrStoreOperationDef[] = [
  {
    value: 'list_collections',
    label: 'List collections',
    fields: [
      { key: 'tags', label: 'Tags', placeholder: 'tag1,tag2' },
      { key: 'tags_op', label: 'Tags operator', placeholder: 'or | and | not' },
      { key: 'limit', label: 'Limit', placeholder: '20' },
      { key: 'offset', label: 'Offset', placeholder: '0' },
    ],
  },
  {
    value: 'search_collections',
    label: 'Search collections',
    fields: [
      { key: 'search', label: 'Search', required: true, placeholder: '{{query}}' },
      { key: 'tags', label: 'Tags', placeholder: 'tag1,tag2' },
      { key: 'tags_op', label: 'Tags operator', placeholder: 'or | and | not' },
      { key: 'limit', label: 'Limit', placeholder: '20' },
      { key: 'offset', label: 'Offset', placeholder: '0' },
    ],
  },
  {
    value: 'list_products',
    label: 'List products',
    fields: [
      { key: 'category_id', label: 'Collection ID', placeholder: '{{category_id}}' },
      { key: 'limit', label: 'Limit', placeholder: '20' },
      { key: 'offset', label: 'Offset', placeholder: '0' },
    ],
  },
  {
    value: 'search_products',
    label: 'Search products',
    fields: [
      { key: 'search', label: 'Search', required: true, placeholder: '{{query}}' },
      { key: 'category_id', label: 'Collection ID', placeholder: '{{category_id}}' },
      { key: 'limit', label: 'Limit', placeholder: '20' },
      { key: 'offset', label: 'Offset', placeholder: '0' },
    ],
  },
  {
    value: 'get_product',
    label: 'Product details',
    fields: [
      { key: 'product_id', label: 'Product ID', required: true, placeholder: '{{product_id}}' },
    ],
  },
  {
    value: 'list_product_options',
    label: 'Product options',
    fields: [
      { key: 'ids', label: 'Option IDs', placeholder: '1,2,3' },
    ],
  },
  { value: 'get_store', label: 'Store details', fields: [] },
  { value: 'get_store_info', label: 'Store info', fields: [] },
  { value: 'list_faqs', label: 'FAQs', fields: [] },
  {
    value: 'check_delivery',
    label: 'Check delivery',
    fields: [
      { key: 'latitude', label: 'Latitude', required: true, placeholder: '{{latitude}}' },
      { key: 'longitude', label: 'Longitude', required: true, placeholder: '{{longitude}}' },
    ],
  },
  {
    value: 'create_order',
    label: 'Checkout (create order)',
    fields: [
      {
        key: 'items',
        label: 'Items JSON',
        required: true,
        multiline: true,
        placeholder: '[{"product_option": {{option_id}}, "quantity": 1}]',
      },
      { key: 'email', label: 'Email', required: true, placeholder: '{{email}}' },
      {
        key: 'delivery_mode',
        label: 'Delivery mode',
        required: true,
        placeholder: 'PICKUP_FROM_STORE or DELIVERY_TO_LOCATION',
      },
      {
        key: 'new_address',
        label: 'New address JSON',
        multiline: true,
        placeholder: '{"name":"{{name}}","address_line_1":"...","city":"...","pincode":"..."}',
      },
      { key: 'notes', label: 'Notes', placeholder: '{{notes}}' },
      { key: 'slot_token', label: 'Slot token', placeholder: '{{slot_token}}' },
    ],
  },
  {
    value: 'get_order',
    label: 'Get order',
    fields: [
      { key: 'order_uuid', label: 'Order UUID', required: true, placeholder: '{{order_uuid}}' },
    ],
  },
  {
    value: 'lookup_order_status',
    label: 'Lookup order status',
    fields: [
      { key: 'order_id', label: 'Order number', placeholder: '{{order_id}}' },
    ],
  },
  {
    value: 'retry_payment',
    label: 'Retry payment',
    fields: [
      { key: 'order_uuid', label: 'Order UUID', required: true, placeholder: '{{order_uuid}}' },
    ],
  },
]

export function tiqrStoreOperationLabel(value: string | undefined): string {
  return TIQR_STORE_OPERATIONS.find((op) => op.value === value)?.label || value || 'No operation'
}

export function tiqrStoreOperationDef(value: string | undefined): TiqrStoreOperationDef | undefined {
  return TIQR_STORE_OPERATIONS.find((op) => op.value === value)
}
