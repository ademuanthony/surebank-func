gcloud functions deploy CreateCustomerHTTP --runtime go113 --trigger-http --allow-unauthenticated
gcloud functions deploy ListCustomerHTTP --runtime go113 --trigger-http --allow-unauthenticated
gcloud functions deploy FindCustomerByIdHTTP --runtime go113 --trigger-http --allow-unauthenticated
gcloud functions deploy CreateAccountHTTP --runtime go113 --trigger-http --allow-unauthenticated
gcloud functions deploy ListAccountHTTP --runtime go113 --trigger-http --allow-unauthenticated
gcloud functions deploy ListDSAccountHTTP --runtime go113 --trigger-http --allow-unauthenticated
gcloud functions deploy ListDebtorsHTTP --runtime go113 --trigger-http --allow-unauthenticated