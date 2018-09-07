'use strict'

var logger = window.log.getLogger({
    name: 'vend',
    level: 'info'
});


/* global $, jQuery, window */
/* eslint-env es6, quotes:single */

// Handles payment flow communication to Vend via the Payments API.
// Documentation: https://docs.vendhq.com/docs/payments-api-reference

// Send postMessage JSON payload to the Payments API.
function sendObjectToVend(object) {
  // Define parent/opener window.
  var receiver = window.opener !== null ? window.opener : window.parent
  // Send JSON object to parent/opener window.

  // this needs to match the site
  // receiver.postMessage(JSON.stringify(object), 'https://amtest.vendhq.com')
  logger.debug("Site " + window.location.search)
  var params = getURLParameters()
  receiver.postMessage(JSON.stringify(object), "*")
}

// Payments API Steps.
// https://docs.vendhq.com/docs/payments-api-reference#section-required
// ACCEPT: Trigger a successful transaction. If the payment type supports
// printing (and it is enabled) an approved transaction receipt will also print,
// containing any of the addition receipt_html_extra that is specified.
// The transaction_id of the external payment should also be specified, as this
// can be later retrieved via the REST API.
function acceptStep(receiptHTML, transactionID) {
  logger.debug('sending ACCEPT step')
  sendObjectToVend({
    step: 'ACCEPT',
    transaction_id: transactionID,
    receipt_html_extra: receiptHTML
  })
}

// DATA: Request additional information from Vend about the sale, payment and
// line items. This is often used to obtain the register_id that processed the
// transaction, as it is the best unique identifier to pair a terminal with.
// This means that if the gateway is storing pairings between a register and a
// terminal, then there is a way to route the payment correctly.
function dataStep() {
  logger.debug('sending DATA step')
  sendObjectToVend({
    step: 'DATA'
  })
}

// DECLINE: Return to pay screen and if enabled a declined transaction receipt
// will also print, including the receipt_html_extra specified. It is important
// to print at this stage to make sure terminal output is included on the
// receipt.
function declineStep(receiptHTML) {
  logger.debug('sending DECLINE step')
  sendObjectToVend({
    step: 'DECLINE',
    print: false,
    receipt_html_extra: receiptHTML
  })
}

  

// EXIT: Cleanly exit the process. Does not close the window but closes all
// other dialoggers including the payment modal/iFrame and unbinds postMessage
// handling. It is better to avoid using this step, as it breaks the transaction
// flow prematurely, and so should only be sent if we are absolutely sure that
// we know the transaction outcome.
function exitStep() {
  logger.debug('sending EXIT step')
  sendObjectToVend({
    step: 'EXIT'
  })
}

// PRINT: Manually trigger a receipt, including any extra information specified.
// This step is not often needed as ACCEPT and DECLINE both include receipt
// printing. It can however be used to print a signature slip midway through
// processing if signature is required by the card verifiction method, in this
// case receipt_html_extra would be used to print a signature line.
function printStep(receiptHTML) {
  logger.debug('sending PRINT step')
  sendObjectToVend({
    step: 'PRINT',
    receipt_html_extra: receiptHTML
  })
}

// SETUP: Customize the payment dialogger. At this stage removing close button to
// prevent cashiers from prematurely closing the modal is advised, as it leads
// to interrupted payment flow without a clean exit.
function setupStep() {
  logger.debug('sending SETUP step')
  sendObjectToVend({
    step: 'SETUP',
    setup: {
      enable_close: false
    }
  })
}

// Get query parameters from the URL. Vend includes amount, origin, and
// register_id.
function getURLParameters() {
  var pageURL = decodeURIComponent(window.location.search.substring(1)),
    params = pageURL.split('&'),
    paramName,
    parameters = {}

  params.forEach(function (param) {
    paramName = param.split('=')

    logger.debug(paramName)

    switch (paramName[0]) {
      case 'amount':
        parameters.amount = paramName[1]
        break
      case 'origin':
        parameters.origin = paramName[1]
        break
      case 'register_id':
        parameters.register_id = paramName[1]
        break
    }
  })

  logger.debug("Parsed URL Params : " + parameters)

  return parameters
}



// Check response status from the gateway, we then manipulate the payment flow
// in Vend in response to this using the Payment API steps.
function checkResponse(response) {
  var response = response;
  logger.info("Response From Server: " + response)
  switch (response.status) {
    case 'ACCEPTED':
        $('#statusMessage').empty()
        var receiptHTML = '';
        console.debug(response)
        
        if (typeof(response.id) != 'undefined') {
            receiptHTML = `
            <div>
                <h2>APPROVED</h2>
                <span>Oxipay Purchase #: ` + response.id+ ` </span>
            </div>`;
        }
      
        acceptStep(receiptHTML, response.id)
      break
    case 'DECLINED':
      $('#statusMessage').empty()
      $.get('/assets/templates/declined.html', function (data) {
        data = data.replace("${response.status}", response.status.toLowerCase());
        data = data.replace("${response.message}", response.message);

        $('#statusMessage').append(data)
      })

      setTimeout(declineStep, 4000, '<div>Declined</div>')
      break
    case 'FAILED':
      
      $('#statusMessage').empty()
      $.get('/assets/templates/failed.html', function (data) {
        response.message = response.message? response.message: "" 
        data = data.replace("${response.status}", response.status.toLowerCase());
        data = data.replace("${response.message}", response.message);
        $('#statusMessage').append(data)
      })
      receiptHTML = `
        <div>
            <h2>DECLINED</h2>
            <span> ` + response.message + `</span>
        </div>`;

      setTimeout(declineStep, 4000, receiptHTML)
      break
    case 'TIMEOUT':
      $('#statusMessage').empty()
      $.get('../assets/templates/timeout.html', function (data) {
        $('#statusMessage').append(data)
      })

      setTimeout(declineStep, 4000,'<div>TIMEOUT</div>')
      break
    default:
      $('#statusMessage').empty()
      $.get('../assets/templates/failed.html', function (data) {
        $('#statusMessage').append(data)
      })

      setTimeout(declineStep, 4000, receiptHTML)
      break
  }
}

var refundDataResponseListener = function (event) {
    
    var result = getURLParameters()

    if (event.origin !== result.origin ) {
        return false;
    }
    
    logger.debug('Received event from Vend')
    logger.debug('Event origin: ' + event.origin)

    var data = JSON.parse(event.data)
    // get sales id. save into a gloabal const

    logger.debug(data)

    var data = {
        amount: result.amount,
        origin: result.origin,
        sale_id: data.register_sale.client_sale_id,
        register_id: result.register_id,
        purchaseno: $("#purchaseno").val()
    };
    
    $.ajax({
        url: '/refund',
        type: 'POST',
        dataType: 'json',
        data: data
    })
    .done(function (response) {
        logger.info(response)

        // Hide outcome buttons while we handle the response.
        $('#outcomes').hide()

        // Check the response body and act according to the payment status.
        checkResponse(response)
    })
    .fail(function (error) {
        logger.error(error)

        // Make sure status text is cleared.
        $('#outcomes').hide()
        $('#statusMessage').empty()
        $.get('../assets/templates/failed.html', function (data) {
          $('#statusMessage').append(data)
        })
        // Quit window, giving cashier chance to try again.
        setTimeout(declineStep, 4000)
    })
}


var paymentDataResponseListener = function (event) {

    var result = getURLParameters()
    
    if (event.origin !== result.origin ) {
        return false;
    }
    
    logger.debug('Received event from Vend')
    logger.debug('Event origin ' + event.origin)

    var data = JSON.parse(event.data)

    // get sales id. save into a gloabal const
    logger.debug("In my event listener " + data)

    var paymentCode = $("#paymentcode").val()

    // Hide outcome buttons.
    $('#outcomes').hide()
  
    // If we did not at least two query params from Vend something is wrong.
    if (Object.keys(result).length < 2) {
      logger.logger('did not get at least two query results')
      $('#statusMessage').empty()
      $.get('../assets/templates/failed.html', function (data) {
        $('#statusMessage').append(data)
      })
      setTimeout(exitStep(), 4000)
    }
    
    $.ajax({
        url: '/pay',
        type: 'POST',
        dataType: 'json',
        data: {
          amount: result.amount,
          origin: result.origin,
          register_id: result.register_id,
          sale_id: data.register_sale.client_sale_id,
          paymentcode: paymentCode
        }
      })
      .done(function (response) {
        
        logger.debug(response)
  
        // Hide outcome buttons while we handle the response.
        $('#outcomes').hide()
  
        // Check the response body and act according to the payment status.
        checkResponse(response)
      })
      .fail(function (error) {
        logger.debug(error)
  
        // Make sure status text is cleared.
        $('#outcomes').hide()
        $('#statusMessage').empty()
        $.get('../assets/templates/failed.html', function (data) {
            $('#statusMessage').append(data)
        })
        // Quit window, giving cashier chance to try again.
        setTimeout(declineStep, 4000)
      })

}

// sendRefund sends refund to the gateway
function sendRefund() {
    // grab the purchase no from form
    var paymentCode = $("#paymentcode").val()
  
    // Hide outcome buttons.
    $('#outcomes').hide()
    
    // Show tap insert or swipe card prompt.
    $('#statusMessage').empty()
    $.get('../assets/templates/payment.html', function (data) {
      $('#statusMessage').append(data)
    })
    // Get the payment context from the URL query string.
    var result = {}
    result = getURLParameters()
  
    // If we did not at least two query params from Vend something is wrong.
    if (Object.keys(result).length < 2) {
      logger.error('did not get at least two query results')
      $('#statusMessage').empty()
      $.get('../assets/templates/failed.html', function (data) {
        $('#statusMessage').append(data)
      })
      setTimeout(exitStep(), 4000)
    }
  
    // We are going to send a data steup so we dynammically bind a listener so that we aren't 
    // subscribing to all events
    window.addEventListener('message', refundDataResponseListener, false)
  
    // send the datastep
    if (inIframe()) {
        dataStep();
    } else  {
        logger.error("It does not appear this is contained in an iframe. This is unexpected and will not currently work.")
        
    }
    
    return false
}

function inIframe () {
    try {
        return window.self !== window.top;
    } catch (e) {
        return true;
    }
}

function sendPayment() {
    // grab the purchase no from form
    var paymentCode = $("#paymentcode").val()
  
    // Hide outcome buttons.
    $('#outcomes').hide()
    
  
    // Show tap insert or swipe card prompt.
    $('#statusMessage').empty()
    $.get('../assets/templates/payment.html', function (data) {
      $('#statusMessage').append(data)
    })
    // Get the payment context from the URL query string.
    var result = {}
    result = getURLParameters()
  
    // If we did not at least two query params from Vend something is wrong.
    if (Object.keys(result).length < 2) {
      logger.logger('did not get at least two query results')
      $('#statusMessage').empty()
      $.get('../assets/templates/failed.html', function (data) {
        $('#statusMessage').append(data)
      })
      setTimeout(exitStep(), 4000)
    }
  
    // We are going to send a data steup so we dynammically bind a listener so that we aren't 
    // subscribing to all events
    window.addEventListener('message', paymentDataResponseListener, false)
      
    // send the datastep
    if (inIframe()) {
      dataStep();
    } else  {
      logger.error("It does not appear this is contained in an iframe. This is unexpected and will not currently work.")
    }
    
    return false
}

function cancelRefund(outcome) {
    logger.info('Cancelling Refund')
  
    // Hide outcome buttons.
    $('#outcomes').hide()
  
    // Show the cancelling with a loader.
    $('#statusMessage').empty()
    $.get('../assets/templates/cancelling.html', function (data) {
      $('#statusMessage').append(data)
    })
  
    // Wait four seconds, then quit window, giving the cashier a chance to try
    // again.
    setTimeout(declineStep, 4000, '<div>CANCELLED</div>')
  }

// cancelPayment simulates cancelling a payment.
function cancelPayment(outcome) {
  logger.info('Cancelling Payment')

  // Hide outcome buttons.
  $('#outcomes').hide()

  // Show the cancelling with a loader.
  $('#statusMessage').empty()
  $.get('../assets/templates/cancelling.html', function (data) {
    $('#statusMessage').append(data)
  })

  // Wait four seconds, then quit window, giving the cashier a chance to try
  // again.
  setTimeout(declineStep, 4000 )
}

function showClose() {
  sendObjectToVend({
    step: 'SETUP',
    setup: {
      enable_close: true
    }
  })
}

// On initial load of modal, configure the page settings such as removing the
// close button and setting the header.
$(function () {

  // Send the SETUP step with our configuration values..  
  setupStep()

  $('#statusMessage').empty()
  $.get('../assets/templates/waiting.html', function (data) {
    $('#statusMessage').append(data)
  })

  // Show outcome buttons.
  $('#outcomes').show()
})