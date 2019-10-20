#!/usr/bin/env perl

# Copyright 2019 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

use strict;
use warnings;

use lib '/home/rt/rt/lib';

use Carp;
use DBI;
use Getopt::Long;
use JSON;
use MIME::Base64;
use Term::ProgressBar;

use RT;
use RT::Queues;
use RT::Tickets;
use RT::Ticket;
use RT::User;

RT::LoadConfig();
RT::Init();

# Disable the logs or it gets very noisy.
$RT::Logger->remove($_) for values $RT::Logger->{outputs};

sub dump_ticket($);

sub main() {
  my $outdir = "/var/tmp/out";
  my $queue = "perl5";
  
  GetOptions(
    "out=s" => \$outdir,
    "queue=s" => \$queue,
  );

  # This is the query for the tickets that will be extracted:
  my $tx = RT::Tickets->new($RT::SystemUser);
  $tx->FromSQL(
    qq[
  Queue = '${queue}'
    AND
  Status != 'deleted'
]
  );
  $tx->OrderBy(FIELD => 'EffectiveId');

  my $progress = Term::ProgressBar->new(
    { name => 'Tickets', count => $tx->Count + 1, remove => 1 });

  my $i = 0;
  while (my $t = $tx->Next) {
    $progress->update($i++);
    my $fn = $outdir . "/" . $t->Id . ".json";
    if (-e $fn) {
      next;
    }

    my $j = dump_ticket($t);
    open my $fh, ">$fn" or die $!;
    print $fh $j;
  }

  write_merged_tickets($outdir);
  $progress->update($i++);
}

sub dump_user($) {
  my $u = shift;

  if (!ref $u) {
    my $newu = RT::User->new(RT::SystemUser);
    $newu->LoadById($u);
    $u = $newu;
  }

  return {
    Name         => $u->Name,
    NickName     => $u->NickName,
    RealName     => $u->RealName,
    EmailAddress => $u->EmailAddress,
    Id           => $u->Id,
  };

}

sub dump_transaction($) {
  my $tx = shift;
  my $h  = _dump_record($tx);
  $h->{Attachments} = [ dump_attachments($tx->Attachments) ];

  return $h;
}

sub dump_attachment($) {
  my $a = shift;
  my @f = qw[
    id
    TransactionId
    Parent
    MessageId
    Subject
    Filename
    ContentType
    Headers
    Creator
    Created
  ];
  my %h = map { $_ => $a->$_ } @f;

  if ($a->ContentType =~ m!^text/!) {
    $h{OriginalContent} = $a->OriginalContent;
  }
  else {
    $h{OriginalContent} = encode_base64($a->OriginalContent);
  }

  return \%h;
}

sub dump_group {
  return map { dump_user($_) } @{ shift->UserMembersObj->ItemsArrayRef };
}

sub dump_record($);

sub dump_record($) {
  my %recordFuncs = (
    "RT::User"        => sub { dump_user(shift); },
    "RT::Date"        => sub { shift->AsString; },
    "RT::Lifecycle"   => sub { "" },
    "RT::Transaction" => sub { dump_transaction(shift); },
    "RT::Queue"       => sub { shift->Name; },
    "RT::Ticket"      => sub { shift->Id; },
  );

  my $l = shift;
  if (!$l) {
    return "";
  }

  my $ref = ref $l;
  if (!$ref) {
    return $l;
  }

  if (defined $recordFuncs{$ref}) {
    return $recordFuncs{$ref}->($l);
  }

  return _dump_record($l);
}

sub _dump_record($) {
  my $l = shift;
  if (!$l) {
    return "";
  }

  my $ref = ref $l;
  if (!$ref) {
    return $l;
  }

  if (!$l->isa("RT::Record")) {
    return "$l";
  }

  my $ca = $l->can($ref . "::_CoreAccessible");
  if (!$ca) {
    croak "Can't deal with '$ref'";
  }
  my @f = keys %{ $ca->($l) };
  my %h = map {
    my $fn = "${_}Obj";
    if (my $f = $l->can($fn)) {
      $_, dump_record($f->($l));
    }
    else {
      if (my $f = $l->can($_)) {
        $_, dump_record($f->($l));
      }
      else {
        $_, $l->$_;
      }
    }
  } @f;
  return \%h;
}

sub dump_records($) {
  my $rs = shift;

  #use Carp; Carp::cluck $rs;

  my @r;
  while (my $r = $rs->Next) {
    push @r, dump_record($r);
  }
  return @r;
}

sub dump_customfield($$) {
  my $t  = shift;
  my $cf = shift;
  return $t->CustomFieldValuesAsString($cf);
}

sub dump_customfields($) {
  my $t   = shift;
  my $cfs = $t->CustomFields;

  my @r;
  while (my $cf = $cfs->Next) {
    push @r, $cf->Name, dump_customfield($t, $cf);
  }
  return @r;
}

sub dump_attachments($) {
  my $txs = shift;

  my @r;
  while (my $tx = $txs->Next) {
    push @r, dump_attachment($tx);
  }
  return @r;
}

sub dump_ticket($) {
  my $t = shift;

  my $h = {
    Id          => $t->Id,            # Are we handling merges properly?
    EffectiveId => $t->EffectiveId,
    Queue       => $t->Queue,
    Type        => $t->Type,

    #IssueStatement =>
    Resolution => $t->Resolution,
    Owner      => dump_user($t->Owner),
    Subject    => $t->Subject,

    #InitialPriority =>
    #FinalPriority =>
    Priority => $t->Priority,

    #TimeEstimated =>
    #TimeWorked =>
    Status => $t->Status,

    #TimeLeft =>
    #Told =>
    #Starts =>
    #Started =>
    #Due =>
    Resolved      => $t->Resolved,                   # epoch 0 if not resolved
    LastUpdatedBy => dump_user($t->LastUpdatedBy),
    LastUpdated   => $t->LastUpdated,
    Creator       => dump_user($t->Creator),
    Created       => $t->Created,

    #Disabled =>
    Transactions => [ dump_records($t->Transactions) ],
    CustomFields => { dump_customfields($t) },

    # TODO: Add Watchers?
    AdminCc    => [ dump_group($t->AdminCc) ],
    Cc         => [ dump_group($t->Cc) ],
    Requestors => [ dump_group($t->Requestors) ],
  };

  my @linkTypes = qw(Members MemberOf RefersTo ReferredToBy
    DependedOnBy DependsOn);
  $h->{Links} = { map { $_ => [ dump_records($t->$_) ] } @linkTypes };

  my $json = JSON->new->allow_nonref->canonical;
  my $j    = $json->pretty->encode($h);

  return $j;
}

# The easiest way to get a list of all the merged tickets is to go
# straight to the database.
sub dump_merged_tickets() {
    my $dbh = $RT::Handle->dbh;
    my $rows = $dbh->selectall_arrayref(
	"SELECT Id,EffectiveID from Tickets where Id!=EffectiveID"

    ) or die $dbh->errstr;

    my %merged = map {
	$_->[0] => $_->[1]
    } @$rows;

    my $json = JSON->new->allow_nonref->canonical;
    return $json->pretty->encode(\%merged);
}

sub write_merged_tickets($) {
    my $outdir = shift;

    my $fn = $outdir . "/" . "merged" . ".json";
    my $j = dump_merged_tickets();
    open my $fh, ">$fn" or die $!;
    print $fh $j;
}

main();
